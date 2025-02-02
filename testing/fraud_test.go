//go:build system_test
// +build system_test

package testing

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRecursiveMsgsExternalTrigger(t *testing.T) {
	sut.ResetDirtyChain(t)
	sut.StartChain(t)
	cli := NewPetriCli(t, sut, verbose)

	codeID := cli.StoreWasm("testing/contract/hackatom.wasm")
	initMsg := fmt.Sprintf(`{"verifier":%q, "beneficiary":%q}`, randomBech32Addr(), randomBech32Addr())
	contractAddr := cli.InstantiateWasm(codeID, initMsg)

	specs := map[string]struct {
		gas           string
		expErrMatcher func(t require.TestingT, err error, msgAndArgs ...interface{})
	}{
		"simulation": {
			gas:           "auto",
			expErrMatcher: ErrOutOfGasMatcher,
		},
		"tx": { // tx will be rejected by Tendermint in post abci checkTX operation
			gas:           strconv.Itoa(math.MaxInt64),
			expErrMatcher: ErrTimeoutMatcher,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			cli := NewPetriCli(t, sut, verbose)
			execMsg := `{"message_loop":{}}`
			for _, n := range sut.AllNodes(t) {
				cli.WithRunErrorMatcher(spec.expErrMatcher).WithNodeAddress(n.RPCAddr()).
					Execute(contractAddr, execMsg, defaultSrcAddr, "--gas="+spec.gas, "--broadcast-mode=sync")
			}
			sut.AwaitNextBlock(t)
		})
	}
}

func TestRecursiveSmartQuery(t *testing.T) {
	sut.ResetDirtyChain(t)
	sut.StartChain(t)
	cli := NewPetriCli(t, sut, verbose)

	initMsg := fmt.Sprintf(`{"verifier":%q, "beneficiary":%q}`, randomBech32Addr(), randomBech32Addr())
	maliciousContractAddr := cli.InstantiateWasm(cli.StoreWasm("testing/contract/hackatom.wasm"), initMsg)

	msg := fmt.Sprintf(`{"recurse":{"depth":%d, "work":0}}`, math.MaxUint32)

	// when
	for _, n := range sut.AllNodes(t) {
		cli.WithRunErrorMatcher(ErrInvalidQuery).WithNodeAddress(n.RPCAddr()).
			QuerySmart(maliciousContractAddr, msg)
	}
	sut.AwaitNextBlock(t)
}

func TestValidatorDoubleSign(t *testing.T) {
	// Scenario:
	//   given: a running chain
	//   when: a second instance with the same val key signs a block
	//   then: the validator is removed from the active set and jailed forever
	cli := NewPetriCli(t, sut, verbose)
	sut.ResetDirtyChain(t)
	sut.StartChain(t)
	byzantineOperatorAddr := cli.GetKeyAddr("node0")
	var validatorPubKey cryptotypes.PubKey

	valsetContractAddr := cli.GetPoEContractAddress("VALSET")

	// Check the validator is in the active set
	rsp := cli.QuerySmart(valsetContractAddr, `{"list_active_validators":{}}`)
	validatorIsActive := strings.Contains(rsp, byzantineOperatorAddr)

	require.True(t, validatorIsActive)

	newNode := sut.AddFullnode(t, func(nodeNumber int, nodePath string) {
		valKeyFile := filepath.Join(workDir, nodePath, "config", "priv_validator_key.json")
		_ = os.Remove(valKeyFile)
		_, err := copyFile(filepath.Join(workDir, sut.nodePath(0), "config", "priv_validator_key.json"), valKeyFile)
		require.NoError(t, err)
		validatorPubKey = loadValidatorPubKeyForNode(t, sut, nodeNumber)
	})
	sut.AwaitNodeUp(t, fmt.Sprintf("http://127.0.0.1:%d", newNode.RPCPort))

	// let's wait some blocks to have evidence and update persisted
	var validatorGotByzantine bool
	for i := 0; i < 15 && !validatorGotByzantine; i++ {
		sut.AwaitNextBlock(t, defaultWaitTime*2)
		rsp := cli.QuerySmart(valsetContractAddr, `{"list_active_validators":{}}`)
		validatorGotByzantine = !strings.Contains(rsp, byzantineOperatorAddr)
	}
	sut.AwaitNextBlock(t)

	require.True(t, validatorGotByzantine)
	rsp = cli.QuerySmart(valsetContractAddr, fmt.Sprintf(`{"validator":{"operator": %q}}`, byzantineOperatorAddr))
	assert.Equal(t, `{"forever":{}}`, gjson.Get(rsp, "data.validator.jailed_until.end").String())
	// and not in tendermint
	valResult, found := cli.IsInTendermintValset(validatorPubKey)
	assert.False(t, found, "not in validator set : %#v", valResult)
}
