package oracleserver

import (
	"autonity-oracle/aggregator"
	cryptoprovider "autonity-oracle/plugin_wrapper"
	pricepool "autonity-oracle/price_pool"
	"autonity-oracle/types"
	"fmt"
	"github.com/hashicorp/go-hclog"
	"github.com/modern-go/reflect2"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"
	"io/fs"
	"io/ioutil" // nolint
	o "os"
	"sync"
	"time"
)

var (
	//ValidDataAge            = 3 * 60 * 1000 // 3 minutes, data fetched within 3 minutes are valid to update the price.
	ValidDataAge            = 1 * 60 * 1000 // 1 minutes, data fetched within 3 minutes are valid to update the price.
	Version                 = "v0.0.1"
	UpdateInterval          = 10 * time.Second // 10s, the data fetching interval for the oracle server's jobTicker job.
	PluginDiscoveryInterval = 2 * time.Second  // 2s, the plugin discovery interval.
)

type OracleServer struct {
	version string
	logger  hclog.Logger

	lockPrices  sync.RWMutex        // mutex to prevent the race condition between ticker job and http service routine.
	prices      types.PriceBySymbol // aggregated prices which is referred by http data service to provide the data service.
	lockPlugins sync.RWMutex        // mutex to prevent the race condition between ticker job and http service routine.
	plugins     types.PluginByName  // save the plugin infos that can be queried via http service.

	doneCh          chan struct{}
	jobTicker       *time.Ticker // the clock source to trigger the 10s interval job.
	discoveryTicker *time.Ticker

	pluginDIR         string                       // the dir saves the plugins.
	lockSymbols       sync.RWMutex                 // mutex to prevent the race condition between symbols writer and reader routine.
	symbols           []string                     // the symbols for data fetching in oracle service.
	aggregator        types.Aggregator             // the price aggregator once we have multiple data providers.
	priceProviderPool *pricepool.PriceProviderPool // the price pool organized by plugin_wrapper and by symbols

	pluginWrappers map[string]types.PluginWrapper // the plugin clients that connect with different adapters.
}

func NewOracleServer(symbols []string, pluginDir string) *OracleServer {
	os := &OracleServer{
		version:           Version,
		symbols:           symbols,
		pluginDIR:         pluginDir,
		prices:            make(types.PriceBySymbol),
		plugins:           make(types.PluginByName),
		pluginWrappers:    make(map[string]types.PluginWrapper),
		doneCh:            make(chan struct{}),
		jobTicker:         time.NewTicker(UpdateInterval),
		discoveryTicker:   time.NewTicker(PluginDiscoveryInterval),
		aggregator:        aggregator.NewAggregator(),
		priceProviderPool: pricepool.NewPriceProviderPool(),
	}

	os.logger = hclog.New(&hclog.LoggerOptions{
		Name:   reflect2.TypeOfPtr(os).String(),
		Output: o.Stdout,
		Level:  hclog.Debug,
	})

	// discover plugins from plugin dir at startup.
	binaries := os.listPluginDIR()
	if len(binaries) == 0 {
		// to stop the service on the start once there is no plugin in the db.
		panic(fmt.Sprintf("No plugins at plugin dir: %s, please build the plugins", os.pluginDIR))
	}
	for _, file := range binaries {
		os.createPlugin(file.Name())
	}

	return os
}

func (os *OracleServer) Version() string {
	return os.version
}

func (os *OracleServer) UpdateSymbols(newSymbols []string) {
	os.lockSymbols.Lock()
	defer os.lockSymbols.Unlock()

	var symbolsMap = make(map[string]struct{})
	for _, s := range os.symbols {
		symbolsMap[s] = struct{}{}
	}

	for _, newS := range newSymbols {
		if _, ok := symbolsMap[newS]; !ok {
			os.symbols = append(os.symbols, newS)
		}
	}
}

func (os *OracleServer) Symbols() []string {
	os.lockSymbols.RLock()
	defer os.lockSymbols.RUnlock()
	return os.symbols
}

func (os *OracleServer) GetPlugins() types.PluginByName {
	os.lockPlugins.RLock()
	defer os.lockPlugins.RUnlock()
	return os.plugins
}

func (os *OracleServer) PutPlugin(name string, plugin types.Plugin) {
	os.lockPlugins.Lock()
	defer os.lockPlugins.Unlock()
	os.plugins[name] = plugin
}

func (os *OracleServer) GetPrices() types.PriceBySymbol {
	os.lockPrices.RLock()
	defer os.lockPrices.RUnlock()
	return os.prices
}

func (os *OracleServer) GetPricesBySymbols(symbols []string) types.PriceBySymbol {
	os.lockPrices.RLock()
	defer os.lockPrices.RUnlock()
	prices := make(types.PriceBySymbol)
	now := time.Now().UnixMilli()
	for _, s := range symbols {
		if p, ok := os.prices[s]; ok {
			// only those price collected within 3 minutes are valid to be exposed.
			if now-p.Timestamp < int64(ValidDataAge) && now >= p.Timestamp {
				prices[s] = p
			} else {
				os.logger.Warn("price is out of update", "symbol", s)
			}
		} else {
			os.logger.Warn("price not available yet", "symbol", s)
		}
	}
	return prices
}

func (os *OracleServer) UpdatePrice(price types.Price) {
	os.lockPrices.Lock()
	defer os.lockPrices.Unlock()
	os.prices[price.Symbol] = price
}

func (os *OracleServer) UpdatePrices(symbols []string) {
	wg := &errgroup.Group{}
	for _, p := range os.pluginWrappers {
		plugin := p
		wg.Go(func() error {
			return plugin.FetchPrices(symbols)
		})
	}
	err := wg.Wait()
	if err != nil {
		os.logger.Error("fetching prices from plugin error", err.Error())
	}

	now := time.Now().UnixMilli()

	for _, s := range symbols {
		var prices []decimal.Decimal
		for _, plugin := range os.pluginWrappers {
			p, err := os.priceProviderPool.GetPriceProvider(plugin.Name()).GetPrice(s)
			if err != nil {
				continue
			}
			// only those price collected within 3 minutes are valid.
			if now-p.Timestamp < int64(ValidDataAge) && now >= p.Timestamp {
				prices = append(prices, p.Price)
			}
		}

		if len(prices) == 0 {
			continue
		}

		price := types.Price{
			Timestamp: now,
			Price:     prices[0],
			Symbol:    s,
		}

		// we have multiple provider provide prices for this symbol, we have to aggregate it.
		if len(prices) > 1 {
			p, err := os.aggregator.Median(prices)
			if err != nil {
				continue
			}
			price.Price = p
		}

		os.UpdatePrice(price)
	}
}

func (os *OracleServer) Stop() {
	os.doneCh <- struct{}{}
	for _, c := range os.pluginWrappers {
		p := c
		p.Close()
	}
}

func (os *OracleServer) Start() {
	// start the ticker job to fetch prices for all the symbols from all pluginWrappers on every 10s.
	// start the ticker jot to discover plugins on every 2s.
	for {
		select {
		case <-os.doneCh:
			os.discoveryTicker.Stop()
			os.jobTicker.Stop()
			os.logger.Info("the jobTicker jobs of oracle service is stopped")
			return
		case <-os.discoveryTicker.C:
			os.PluginRuntimeDiscovery()
		case <-os.jobTicker.C:
			os.UpdatePrices(os.Symbols())
		}
	}
}

func (os *OracleServer) PluginRuntimeDiscovery() {
	binaries := os.listPluginDIR()

	for _, file := range binaries {
		plugin, ok := os.pluginWrappers[file.Name()]
		if !ok {
			os.logger.Info("** New plugin discovered, going to setup it: ", file.Name(), file.Mode().String())
			os.createPlugin(file.Name())
			os.logger.Info("** New plugin on ready: ", file.Name())
			continue
		}

		if file.ModTime().After(plugin.StartTime()) {
			os.logger.Info("*** Replacing legacy plugin with new one: ", file.Name(), file.Mode().String())
			// stop the legacy plugins process, disconnect rpc connection and release memory.
			plugin.Close()
			delete(os.pluginWrappers, file.Name())
			os.createPlugin(file.Name())
			os.logger.Info("*** Finnish the replacement of plugin: ", file.Name())
		}
	}
}

func (os *OracleServer) createPlugin(name string) {
	pool := os.priceProviderPool.GetPriceProvider(name)
	if pool == nil {
		pool = os.priceProviderPool.AddPriceProvider(name)
	}

	pluginWrapper := cryptoprovider.NewPluginWrapper(name, os.pluginDIR, pool)
	pluginWrapper.Initialize()

	os.pluginWrappers[name] = pluginWrapper

	os.PutPlugin(pluginWrapper.Name(), types.Plugin{
		Version: pluginWrapper.Version(),
		Name:    pluginWrapper.Name(),
		StartAt: pluginWrapper.StartTime(),
	})
}

func (os *OracleServer) listPluginDIR() []fs.FileInfo {
	var plugins []fs.FileInfo

	files, err := ioutil.ReadDir(os.pluginDIR)
	if err != nil {
		os.logger.Error("cannot read from plugin store, please double check plugins are saved in the directory: ",
			os.pluginDIR, err.Error())
		return nil
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		plugins = append(plugins, file)
	}
	return plugins
}
