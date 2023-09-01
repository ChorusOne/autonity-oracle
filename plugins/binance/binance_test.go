package main

import (
	"autonity-oracle/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewBIClient(t *testing.T) {
	var conf = types.PluginConfig{
		Name: "binance",
	}

	resolveConf(&conf)

	client := NewBIClient(&conf)
	prices, err := client.FetchPrice([]string{"BTCUSD", "ETHUSD"})
	require.NoError(t, err)
	require.Equal(t, 2, len(prices))
}

func TestBIClient_AvailableSymbols(t *testing.T) {
	var conf = types.PluginConfig{
		Name: "binance",
	}

	resolveConf(&conf)

	client := NewBIClient(&conf)
	symbols, err := client.AvailableSymbols()
	require.NoError(t, err)

	require.Contains(t, symbols, "BTCUSD")
}