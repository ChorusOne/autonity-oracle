package types

import (
	"github.com/hashicorp/go-plugin"
	"log"
	"net/rpc"
)

// This file defines the autonity oracle plugins specification on top of go-plugin framework which leverage the localhost
// net rpc, or grpc for plugin integrations.

// HandshakeConfig are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user-friendly error is shown.
// This prevents users from executing bad plugins or executing a plugin
// directory. It is a UX feature, not a security feature.
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "BASIC_PLUGIN",
	MagicCookieValue: "hello",
}

// Adapter is the interface that we're exposing as a plugin.
type Adapter interface {
	FetchPrices(symbols []string) []Price
}

// AdapterRPCClient is an implementation that talks over RPC client
type AdapterRPCClient struct{ client *rpc.Client }

func (g *AdapterRPCClient) FetchPrices(symbols []string) []Price {
	var resp []Price
	err := g.client.Call("Plugin.FetchPrices", symbols, &resp)
	if err != nil {
		log.Printf("cannot call plugin: %s.", err.Error())
	}

	return resp
}

// AdapterRPCServer Here is the RPC server that AdapterRPCClient talks to, conforming to the requirements of net/rpc
type AdapterRPCServer struct {
	// This is the real implementation
	Impl Adapter
}

func (s *AdapterRPCServer) FetchPrices(symbols []string, resp *[]Price) error {
	*resp = s.Impl.FetchPrices(symbols)
	return nil
}

// This is the implementation of plugin.Plugin so we can serve/consume this
//
// This has two methods: Server must return an RPC server for this plugin
// type. We construct a AdapterRPCServer for this.
//
// Client must return an implementation of our interface that communicates
// over an RPC client. We return AdapterRPCClient for this.
//
// Ignore MuxBroker. That is used to create more multiplexed streams on our
// plugin connection and is a more advanced use case.
type AdapterPlugin struct {
	// Impl Injection
	Impl Adapter
}

func (p *AdapterPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &AdapterRPCServer{Impl: p.Impl}, nil
}

func (AdapterPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &AdapterRPCClient{client: c}, nil
}
