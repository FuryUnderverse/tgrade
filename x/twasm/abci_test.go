package twasm

import (
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/types/address"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"

	"github.com/oldfurya/furya/x/twasm/keeper"
	"github.com/oldfurya/furya/x/twasm/types"
)

func TestBeginBlock(t *testing.T) {
	var (
		capturedSudoCalls []tuple
		myAddr            = keeper.RandomAddress(t)
		myOtherAddr       = keeper.RandomAddress(t)
		myOtherAddrBase64 = make([]byte, base64.StdEncoding.EncodedLen(address.Len))
		myTime            = time.Unix(1000000000, 0)
	)
	base64.StdEncoding.Encode(myOtherAddrBase64, myOtherAddr)

	specs := map[string]struct {
		setup        func(m *MockSudoer)
		src          abci.RequestBeginBlock
		expSudoCalls []tuple
		expPanic     bool
		expCommitted []bool
	}{
		"single callback": {
			setup: func(m *MockSudoer) {
				m.SudoFn = captureSudos(&capturedSudoCalls)
				m.IteratePrivilegedContractsByTypeFn = iterateContractsFn(t, types.PrivilegeTypeBeginBlock, myAddr)
			},
			expSudoCalls: []tuple{{addr: myAddr, msg: []byte(`{"begin_block":{"evidence":[]}}`)}},
			expCommitted: []bool{true},
		},
		"multiple callbacks": {
			setup: func(m *MockSudoer) {
				m.SudoFn = captureSudos(&capturedSudoCalls)
				m.IteratePrivilegedContractsByTypeFn = iterateContractsFn(t, types.PrivilegeTypeBeginBlock, myAddr, myOtherAddr)
			},
			expSudoCalls: []tuple{
				{addr: myAddr, msg: []byte(`{"begin_block":{"evidence":[]}}`)},
				{addr: myOtherAddr, msg: []byte(`{"begin_block":{"evidence":[]}}`)},
			},
			expCommitted: []bool{true, true},
		},
		"no callback": {
			setup: func(m *MockSudoer) {
				m.IteratePrivilegedContractsByTypeFn = iterateContractsFn(t, types.PrivilegeTypeBeginBlock)
			},
		},
		"with evidence - light client": {
			setup: func(m *MockSudoer) {
				m.SudoFn = captureSudos(&capturedSudoCalls)
				m.IteratePrivilegedContractsByTypeFn = iterateContractsFn(t, types.PrivilegeTypeBeginBlock, myAddr)
			},
			src: abci.RequestBeginBlock{
				ByzantineValidators: []abci.Evidence{
					{
						Type:             abci.EvidenceType_LIGHT_CLIENT_ATTACK,
						Validator:        abci.Validator{Address: myOtherAddr, Power: 1},
						Height:           1,
						Time:             myTime,
						TotalVotingPower: 1,
					},
				},
			},
			expSudoCalls: []tuple{{
				addr: myAddr,
				msg:  []byte(fmt.Sprintf(`{"begin_block":{"evidence":[{"evidence_type":"light_client_attack","validator":{"address": %q, "power": 1}, "height": 1, "time": 1000000000, "total_voting_power": 1}]}}`, myOtherAddrBase64)),
			}},
			expCommitted: []bool{true},
		},
		"with evidence - duplicate vote": {
			setup: func(m *MockSudoer) {
				m.SudoFn = captureSudos(&capturedSudoCalls)
				m.IteratePrivilegedContractsByTypeFn = iterateContractsFn(t, types.PrivilegeTypeBeginBlock, myAddr)
			},
			src: abci.RequestBeginBlock{
				ByzantineValidators: []abci.Evidence{
					{
						Type:             abci.EvidenceType_DUPLICATE_VOTE,
						Validator:        abci.Validator{Address: myOtherAddr, Power: 1},
						Height:           1,
						Time:             myTime,
						TotalVotingPower: 1,
					},
				},
			},
			expSudoCalls: []tuple{{
				addr: myAddr,
				msg:  []byte(fmt.Sprintf(`{"begin_block":{"evidence":[{"evidence_type":"duplicate_vote","validator":{"address": %q, "power": 1}, "height": 1, "time": 1000000000, "total_voting_power": 1}]}}`, myOtherAddrBase64)),
			}},
			expCommitted: []bool{true},
		},
		"with evidence - unknown type ignored": {
			setup: func(m *MockSudoer) {
				m.SudoFn = captureSudos(&capturedSudoCalls)
				m.IteratePrivilegedContractsByTypeFn = iterateContractsFn(t, types.PrivilegeTypeBeginBlock, myAddr)
			},
			src: abci.RequestBeginBlock{
				ByzantineValidators: []abci.Evidence{
					{
						Type: abci.EvidenceType_UNKNOWN,
					},
				},
			},
			expSudoCalls: []tuple{{addr: myAddr, msg: []byte(`{"begin_block":{"evidence":[]}}`)}},
			expCommitted: []bool{true},
		},
		"sudo return error - handled": {
			setup: func(m *MockSudoer) {
				m.SudoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress, msg []byte) ([]byte, error) {
					if contractAddress.Equals(myAddr) {
						return nil, errors.New("test - ignore")
					}
					return captureSudos(&capturedSudoCalls)(ctx, contractAddress, msg)
				}
				m.IteratePrivilegedContractsByTypeFn = iterateContractsFn(t, types.PrivilegeTypeBeginBlock, myAddr, myOtherAddr)
			},
			expSudoCalls: []tuple{{addr: myOtherAddr, msg: []byte(`{"begin_block":{"evidence":[]}}`)}},
			expCommitted: []bool{false, true},
		},
		"sudo panics - handled": {
			setup: func(m *MockSudoer) {
				m.SudoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress, msg []byte) ([]byte, error) {
					if contractAddress.Equals(myAddr) {
						panic("testing")
					}
					return captureSudos(&capturedSudoCalls)(ctx, contractAddress, msg)
				}
				m.IteratePrivilegedContractsByTypeFn = iterateContractsFn(t, types.PrivilegeTypeBeginBlock, myAddr, myOtherAddr)
			},
			expSudoCalls: []tuple{{addr: myOtherAddr, msg: []byte(`{"begin_block":{"evidence":[]}}`)}},
			expCommitted: []bool{false, true},
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			capturedSudoCalls = nil
			mock := MockSudoer{}
			spec.setup(&mock)
			commitMultistore := mockCommitMultiStore{}
			ctx := sdk.Context{}.WithLogger(log.TestingLogger()).
				WithMultiStore(&commitMultistore)

			// when
			if spec.expPanic {
				require.Panics(t, func() {
					BeginBlocker(ctx, &mock, spec.src)
				})
				return
			}
			BeginBlocker(ctx, &mock, spec.src)

			// then
			require.Len(t, capturedSudoCalls, len(spec.expSudoCalls))
			for i, v := range spec.expSudoCalls {
				require.Equal(t, v.addr, capturedSudoCalls[i].addr)
				exp, got := string(v.msg), string(capturedSudoCalls[i].msg)
				assert.JSONEq(t, exp, got, "expected %q but got %q", exp, got)
			}
			// and tx committed
			for i, v := range spec.expCommitted {
				assert.Equal(t, v, commitMultistore.committed[i], "tx number %d", i)
			}
		})
	}
}

func TestEndBlock(t *testing.T) {
	var (
		capturedSudoCalls []tuple
		myAddr            = keeper.RandomAddress(t)
		myOtherAddr       = keeper.RandomAddress(t)
	)

	specs := map[string]struct {
		setup        func(m *MockSudoer)
		expSudoCalls []tuple
		expPanic     bool
		expCommitted []bool
	}{
		"end block - single callback": {
			setup: func(m *MockSudoer) {
				m.SudoFn = captureSudos(&capturedSudoCalls)
				m.IteratePrivilegedContractsByTypeFn = endBlockTypeIterateContractsFn(t, []sdk.AccAddress{myAddr}, nil)
			},
			expSudoCalls: []tuple{{addr: myAddr, msg: []byte(`{"end_block":{}}`)}},
			expCommitted: []bool{true},
		},
		"end block - multiple callbacks": {
			setup: func(m *MockSudoer) {
				m.SudoFn = captureSudos(&capturedSudoCalls)
				m.IteratePrivilegedContractsByTypeFn = endBlockTypeIterateContractsFn(t, []sdk.AccAddress{myAddr, myOtherAddr}, nil)
			},
			expSudoCalls: []tuple{
				{addr: myAddr, msg: []byte(`{"end_block":{}}`)},
				{addr: myOtherAddr, msg: []byte(`{"end_block":{}}`)},
			},
			expCommitted: []bool{true, true},
		},
		"no callback": {
			setup: func(m *MockSudoer) {
				m.IteratePrivilegedContractsByTypeFn = endBlockTypeIterateContractsFn(t, nil, nil)
			},
		},
		"end block - sudo return error handled": {
			setup: func(m *MockSudoer) {
				m.SudoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress, msg []byte) ([]byte, error) {
					if contractAddress.Equals(myAddr) {
						return nil, errors.New("test - ignore")
					}
					return captureSudos(&capturedSudoCalls)(ctx, contractAddress, msg)
				}
				m.IteratePrivilegedContractsByTypeFn = endBlockTypeIterateContractsFn(t, []sdk.AccAddress{myAddr, myOtherAddr}, nil)
			},
			expSudoCalls: []tuple{{addr: myOtherAddr, msg: []byte(`{"end_block":{}}`)}},
			expCommitted: []bool{false, true},
		},
		"end block - sudo panic handled": {
			setup: func(m *MockSudoer) {
				m.SudoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress, msg []byte) ([]byte, error) {
					if contractAddress.Equals(myAddr) {
						panic("testing")
					}
					return captureSudos(&capturedSudoCalls)(ctx, contractAddress, msg)
				}
				m.IteratePrivilegedContractsByTypeFn = endBlockTypeIterateContractsFn(t, []sdk.AccAddress{myAddr, myOtherAddr}, nil)
			},
			expSudoCalls: []tuple{{addr: myOtherAddr, msg: []byte(`{"end_block":{}}`)}},
			expCommitted: []bool{false, true},
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			capturedSudoCalls = nil
			mock := MockSudoer{}
			spec.setup(&mock)
			commitMultistore := mockCommitMultiStore{}
			ctx := sdk.Context{}.WithLogger(log.TestingLogger()).
				WithMultiStore(&commitMultistore)

			// when
			if spec.expPanic {
				require.Panics(t, func() {
					_ = EndBlocker(ctx, &mock)
				})
				return
			}
			gotValsetUpdate := EndBlocker(ctx, &mock)
			require.Empty(t, gotValsetUpdate)

			// then
			require.Len(t, capturedSudoCalls, len(spec.expSudoCalls))
			for i, v := range spec.expSudoCalls {
				require.Equal(t, v.addr, capturedSudoCalls[i].addr)
				exp, got := string(v.msg), string(capturedSudoCalls[i].msg)
				assert.JSONEq(t, exp, got, "expected %q but got %q", exp, got)
			}
			// and tx committed
			for i, v := range spec.expCommitted {
				assert.Equal(t, v, commitMultistore.committed[i], "tx number %d", i)
			}
		})
	}
}

func iterateContractsFn(t *testing.T, expType types.PrivilegeType, addrs ...sdk.AccAddress) func(ctx sdk.Context, callbackType types.PrivilegeType, cb func(prio uint8, contractAddr sdk.AccAddress) bool) {
	return func(ctx sdk.Context, callbackType types.PrivilegeType, cb func(prio uint8, contractAddr sdk.AccAddress) bool) {
		require.Equal(t, expType, callbackType)
		for i, a := range addrs {
			if cb(uint8(i+1), a) {
				return
			}
		}
	}
}

// helper function to handle both types in end block
func endBlockTypeIterateContractsFn(t *testing.T, end []sdk.AccAddress, valset []sdk.AccAddress) func(sdk.Context, types.PrivilegeType, func(uint8, sdk.AccAddress) bool) {
	return func(ctx sdk.Context, callbackType types.PrivilegeType, cb func(prio uint8, contractAddr sdk.AccAddress) bool) {
		switch callbackType {
		case types.PrivilegeTypeEndBlock:
			iterateContractsFn(t, types.PrivilegeTypeEndBlock, end...)(ctx, callbackType, cb)
		case types.PrivilegeTypeValidatorSetUpdate:
			iterateContractsFn(t, types.PrivilegeTypeValidatorSetUpdate, valset...)(ctx, callbackType, cb)
		default:
			t.Errorf("unexpected callback type: %q", callbackType.String())
		}
	}
}

type tuple struct {
	addr sdk.AccAddress
	msg  []byte
}

func captureSudos(capturedSudoCalls *[]tuple) func(ctx sdk.Context, contractAddress sdk.AccAddress, msg []byte) ([]byte, error) {
	return func(ctx sdk.Context, contractAddress sdk.AccAddress, msg []byte) ([]byte, error) {
		*capturedSudoCalls = append(*capturedSudoCalls, tuple{addr: contractAddress, msg: msg})
		return nil, nil
	}
}

type MockSudoer struct {
	SudoFn                             func(ctx sdk.Context, contractAddress sdk.AccAddress, msg []byte) ([]byte, error)
	IteratePrivilegedContractsByTypeFn func(ctx sdk.Context, privilegeType types.PrivilegeType, cb func(prio uint8, contractAddr sdk.AccAddress) bool)
}

func (m MockSudoer) Sudo(ctx sdk.Context, contractAddress sdk.AccAddress, msg []byte) ([]byte, error) {
	if m.SudoFn == nil {
		panic("not expected to be called")
	}
	return m.SudoFn(ctx, contractAddress, msg)
}

func (m MockSudoer) IteratePrivilegedContractsByType(ctx sdk.Context, privilegeType types.PrivilegeType, cb func(prio uint8, contractAddr sdk.AccAddress) bool) {
	if m.IteratePrivilegedContractsByTypeFn == nil {
		panic("not expected to be called")
	}
	m.IteratePrivilegedContractsByTypeFn(ctx, privilegeType, cb)
}

type mockCommitMultiStore struct {
	sdk.CommitMultiStore
	committed []bool
}

func (m *mockCommitMultiStore) CacheMultiStore() storetypes.CacheMultiStore {
	m.committed = append(m.committed, false)
	return &mockCMS{m, &m.committed[len(m.committed)-1]}
}

type mockCMS struct {
	sdk.CommitMultiStore
	committed *bool
}

func (m *mockCMS) Write() {
	*m.committed = true
}
