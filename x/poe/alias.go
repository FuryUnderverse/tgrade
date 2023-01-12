// nolint

package poe

import (
	"github.com/oldfurya/furya/x/poe/keeper"
	"github.com/oldfurya/furya/x/poe/types"
)

const (
	ModuleName = types.ModuleName
	StoreKey   = types.StoreKey
	RouterKey  = types.RouterKey
)

type DeliverTxfn = keeper.DeliverTxFn
