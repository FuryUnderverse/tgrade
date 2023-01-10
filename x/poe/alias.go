// nolint

package poe

import (
	"github.com/blackfury-1/petri/x/poe/keeper"
	"github.com/blackfury-1/petri/x/poe/types"
)

const (
	ModuleName = types.ModuleName
	StoreKey   = types.StoreKey
	RouterKey  = types.RouterKey
)

type DeliverTxfn = keeper.DeliverTxFn
