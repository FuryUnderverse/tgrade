package testing

import (
	"path/filepath"
	"testing"

	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/privval"
)

// load validator nodes consensus pub key for given node number
func loadValidatorPubKeyForNode(t *testing.T, sut *SystemUnderTest, nodeNumber int) cryptotypes.PubKey { //nolint:unused,deadcode
	return loadValidatorPubKey(t, filepath.Join(workDir, sut.nodePath(nodeNumber), "config", "priv_validator_key.json"))
}

// load validator nodes consensus pub key from disk
func loadValidatorPubKey(t *testing.T, keyFile string) cryptotypes.PubKey { //nolint:unused,deadcode
	filePV := privval.LoadFilePVEmptyState(keyFile, "")
	pubKey, err := filePV.GetPubKey()
	require.NoError(t, err)
	valPubKey, err := cryptocodec.FromTmPubKeyInterface(pubKey)
	require.NoError(t, err)
	return valPubKey
}

// queryTendermintValidatorPower returns the validator's power from tendermint RPC endpoint. 0 when not found
func queryTendermintValidatorPower(t *testing.T, sut *SystemUnderTest, nodeNumber int) int64 { //nolint:unused,deadcode
	pubKey := loadValidatorPubKeyForNode(t, sut, nodeNumber)
	valResult := NewPetriCli(t, sut, false).GetTendermintValidatorSet()
	for _, v := range valResult.Validators {
		if v.PubKey.Equals(pubKey) {
			return v.VotingPower
		}
	}
	return 0
}
