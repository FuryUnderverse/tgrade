package testing

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	poetypes "github.com/oldfurya/furya/x/poe/types"
)

// SetPoEParamsMutator set params in genesis
func SetPoEParamsMutator(t *testing.T, params poetypes.Params) GenesisMutator {
	return func(genesis []byte) []byte {
		t.Helper()
		val, err := json.Marshal(params)
		require.NoError(t, err)
		state, err := sjson.SetRawBytes(genesis, "app_state.poe.params", val)
		require.NoError(t, err)
		return state
	}
}

// SetGlobalMinFee set the passed coins to the global minimum fee
func SetGlobalMinFee(t *testing.T, fees ...sdk.DecCoin) GenesisMutator {
	return func(genesis []byte) []byte {
		t.Helper()
		coins := sdk.NewDecCoins(fees...)
		require.NoError(t, coins.Validate())
		val, err := json.Marshal(coins)
		require.NoError(t, err)
		state, err := sjson.SetRawBytes(genesis, "app_state.globalfee.params.minimum_gas_prices", val)
		require.NoError(t, err)
		return state
	}
}

// SetAllEngagementPoints set the given value for all members of the engagement group (default = validators)
func SetAllEngagementPoints(t *testing.T, points int) GenesisMutator {
	return func(raw []byte) []byte {
		group := gjson.GetBytes(raw, "app_state.poe.seed_contracts.engagement").Array()
		for i := range group {
			var err error
			raw, err = sjson.SetRawBytes(raw, fmt.Sprintf("app_state.poe.seed_contracts.engagement.%d.points", i), []byte(fmt.Sprintf(`"%d"`, points)))
			require.NoError(t, err)
		}
		return raw
	}
}

// SetEpochLength set the valset contract config to given epoch length
func SetEpochLength(t *testing.T, epoch time.Duration) GenesisMutator {
	return func(genesis []byte) []byte {
		t.Helper()
		state, err := sjson.SetRawBytes(genesis, "app_state.poe.seed_contracts.valset_contract_config.epoch_length", []byte(fmt.Sprintf("%q", epoch)))
		require.NoError(t, err)
		return state
	}
}

// SetUnbondingPeriod set the stake contract config unbonding period
func SetUnbondingPeriod(t *testing.T, unbonding time.Duration) GenesisMutator {
	return func(genesis []byte) []byte {
		t.Helper()
		state, err := sjson.SetRawBytes(genesis, "app_state.poe.seed_contracts.stake_contract_config.unbonding_period", []byte(fmt.Sprintf("%q", unbonding)))
		require.NoError(t, err)
		return state
	}
}

// SetBlockRewards set the valset contract config unbonding period
func SetBlockRewards(t *testing.T, amount sdk.Coin) GenesisMutator {
	return func(genesis []byte) []byte {
		t.Helper()
		bz, err := json.Marshal(amount)
		require.NoError(t, err)
		state, err := sjson.SetRawBytes(genesis, "app_state.poe.seed_contracts.valset_contract_config.epoch_reward", bz)
		require.NoError(t, err)
		return state
	}
}

// SetConsensusMaxGas max gas that can be consumed in a block
func SetConsensusMaxGas(t *testing.T, max int) GenesisMutator {
	return func(genesis []byte) []byte {
		t.Helper()
		state, err := sjson.SetRawBytes(genesis, "consensus_params.block.max_gas", []byte(fmt.Sprintf(`"%d"`, max)))
		require.NoError(t, err)
		return state
	}
}
