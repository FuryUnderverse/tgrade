package keeper

import (
	"bytes"
	"errors"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/address"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/CosmWasm/wasmd/x/wasm/keeper/wasmtesting"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	cosmwasm "github.com/CosmWasm/wasmvm"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/rand"

	"github.com/oldfurya/furya/x/twasm/contract"
	"github.com/oldfurya/furya/x/twasm/types"
)

func TestSetPrivileged(t *testing.T) {
	var (
		capturedPinChecksum  *cosmwasm.Checksum
		capturedSudoChecksum *cosmwasm.Checksum
		capturedSudoMsg      []byte
	)
	specs := map[string]struct {
		setup  func(*wasmtesting.MockWasmer)
		expErr bool
	}{
		"all good": {
			setup: func(mock *wasmtesting.MockWasmer) {
				mock.PinFn = func(checksum cosmwasm.Checksum) error {
					capturedPinChecksum = &checksum
					return nil
				}
				mock.SudoFn = func(codeID cosmwasm.Checksum, env wasmvmtypes.Env, sudoMsg []byte, store cosmwasm.KVStore, goapi cosmwasm.GoAPI, querier cosmwasm.Querier, gasMeter cosmwasm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
					capturedSudoChecksum = &codeID
					capturedSudoMsg = sudoMsg
					return &wasmvmtypes.Response{}, 0, nil
				}
			},
		},
		"pin failed": {
			setup: func(mock *wasmtesting.MockWasmer) {
				mock.PinFn = func(checksum cosmwasm.Checksum) error {
					return errors.New("test, ignore")
				}
			},
			expErr: true,
		},
		"sudo msg failed": {
			setup: func(mock *wasmtesting.MockWasmer) {
				mock.PinFn = func(checksum cosmwasm.Checksum) error {
					return nil
				}
				mock.SudoFn = func(codeID cosmwasm.Checksum, env wasmvmtypes.Env, sudoMsg []byte, store cosmwasm.KVStore, goapi cosmwasm.GoAPI, querier cosmwasm.Querier, gasMeter cosmwasm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
					return nil, 0, errors.New("test, ignore")
				}
			},
			expErr: true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			capturedPinChecksum, capturedSudoMsg, capturedSudoMsg = nil, nil, nil
			mock := NewWasmVMMock()
			spec.setup(mock)

			ctx, keepers := CreateDefaultTestInput(t, wasmkeeper.WithWasmEngine(mock))
			k := keepers.TWasmKeeper

			codeID, contractAddr := seedTestContract(t, ctx, k)

			// when
			err := k.SetPrivileged(ctx, contractAddr)

			// then
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			var expChecksum cosmwasm.Checksum = k.GetCodeInfo(ctx, codeID).CodeHash

			// then expect pinned to cache
			assert.Equal(t, expChecksum, *capturedPinChecksum)
			// and flag set
			assert.True(t, k.IsPrivileged(ctx, contractAddr))
			// and sudo called
			assert.Equal(t, expChecksum, *capturedSudoChecksum)
			assert.JSONEq(t, `{"privilege_change":{"promoted":{}}}`, string(capturedSudoMsg), "got %s", string(capturedSudoMsg))
		})
	}
}

func TestUnsetPrivileged(t *testing.T) {
	var (
		capturedUnpinChecksum *cosmwasm.Checksum
		capturedSudoChecksum  *cosmwasm.Checksum
		capturedSudoMsg       []byte
	)
	captureSudoFn := func(codeID cosmwasm.Checksum, env wasmvmtypes.Env, sudoMsg []byte, store cosmwasm.KVStore, goapi cosmwasm.GoAPI, querier cosmwasm.Querier, gasMeter cosmwasm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
		capturedSudoChecksum = &codeID
		capturedSudoMsg = sudoMsg
		return &wasmvmtypes.Response{}, 0, nil
	}

	specs := map[string]struct {
		setup    func(t *testing.T, ctx sdk.Context, keepers TestKeepers, mock *wasmtesting.MockWasmer)
		expErr   bool
		expUnpin bool
	}{
		"all good": {
			setup: func(t *testing.T, ctx sdk.Context, keepers TestKeepers, mock *wasmtesting.MockWasmer) {
				mock.UnpinFn = func(checksum cosmwasm.Checksum) error {
					capturedUnpinChecksum = &checksum
					return nil
				}
				mock.SudoFn = captureSudoFn
			},
			expUnpin: true,
		},
		"unpin failed": {
			setup: func(t *testing.T, ctx sdk.Context, keepers TestKeepers, mock *wasmtesting.MockWasmer) {
				mock.UnpinFn = func(checksum cosmwasm.Checksum) error {
					return errors.New("test, ignore")
				}
				mock.SudoFn = func(codeID cosmwasm.Checksum, env wasmvmtypes.Env, sudoMsg []byte, store cosmwasm.KVStore, goapi cosmwasm.GoAPI, querier cosmwasm.Querier, gasMeter cosmwasm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
					return &wasmvmtypes.Response{}, 0, nil
				}
			},
			expErr: true,
		},
		"sudo failed": {
			setup: func(t *testing.T, ctx sdk.Context, keepers TestKeepers, mock *wasmtesting.MockWasmer) {
				mock.SudoFn = func(codeID cosmwasm.Checksum, env wasmvmtypes.Env, sudoMsg []byte, store cosmwasm.KVStore, goapi cosmwasm.GoAPI, querier cosmwasm.Querier, gasMeter cosmwasm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
					return nil, 0, errors.New("test, ignore")
				}
			},
			expErr: true,
		},
		"do not unpin with instances": {
			setup: func(t *testing.T, ctx sdk.Context, keepers TestKeepers, mock *wasmtesting.MockWasmer) {
				mock.SudoFn = captureSudoFn
				mock.UnpinFn = func(checksum cosmwasm.Checksum) error {
					panic("not expected to be call")
				}
				// add another instance for same code id
				creatorAddr := sdk.AccAddress(rand.Bytes(address.Len))
				_, _, err := keepers.TWasmKeeper.GetContractKeeper().Instantiate(ctx, 1, creatorAddr, creatorAddr, nil, "", nil)
				require.NoError(t, err)
			},
			expUnpin: false,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			capturedUnpinChecksum, capturedSudoMsg, capturedSudoMsg = nil, nil, nil
			mock := NewWasmVMMock()
			ctx, keepers := CreateDefaultTestInput(t, wasmkeeper.WithWasmEngine(mock))
			k := keepers.TWasmKeeper
			codeID, contractAddr := seedTestContract(t, ctx, k)
			spec.setup(t, ctx, keepers, mock)

			h := NewPetriHandler(nil, k, nil, nil, nil)
			// and privileged with a type
			k.setPrivilegedFlag(ctx, contractAddr)
			err := h.handlePrivilege(ctx, contractAddr, &contract.PrivilegeMsg{
				Request: types.PrivilegeTypeBeginBlock,
			})
			require.NoError(t, err)
			err = h.handlePrivilege(ctx, contractAddr, &contract.PrivilegeMsg{
				Request: types.PrivilegeTypeEndBlock,
			})
			require.NoError(t, err)

			// when
			err = k.UnsetPrivileged(ctx, contractAddr)

			// then
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			var expChecksum cosmwasm.Checksum = k.GetCodeInfo(ctx, codeID).CodeHash

			if spec.expUnpin {
				// then expect unpinned from cache
				assert.Equal(t, expChecksum, *capturedUnpinChecksum)
			}
			// and flag not set
			assert.False(t, k.IsPrivileged(ctx, contractAddr))
			// and privileges removed
			assert.False(t, k.ExistsAnyPrivilegedContract(ctx, types.PrivilegeTypeEndBlock))
			assert.False(t, k.ExistsAnyPrivilegedContract(ctx, types.PrivilegeTypeBeginBlock))
			// and sudo called
			assert.Equal(t, expChecksum, *capturedSudoChecksum)
			assert.JSONEq(t, `{"privilege_change":{"demoted":{}}}`, string(capturedSudoMsg), "got %s", string(capturedSudoMsg))
			// and state updated
			info := k.GetContractInfo(ctx, contractAddr)
			var details types.PetriContractDetails
			require.NoError(t, info.ReadExtension(&details))
			assert.Empty(t, details.RegisteredPrivileges)
		})
	}
}

func TestIteratePrivileged(t *testing.T) {
	ctx, keepers := CreateDefaultTestInput(t, wasmkeeper.WithWasmEngine(NewWasmVMMock()))
	k := keepers.TWasmKeeper

	var (
		addr1 = sdk.AccAddress(bytes.Repeat([]byte{1}, address.Len))
		addr2 = sdk.AccAddress(bytes.Repeat([]byte{2}, address.Len))
		addr3 = sdk.AccAddress(bytes.Repeat([]byte{3}, address.Len))
	)
	for _, a := range []sdk.AccAddress{addr2, addr1, addr3} {
		k.setPrivilegedFlag(ctx, a)
	}

	var captured []sdk.AccAddress

	specs := map[string]struct {
		callback func(addr sdk.AccAddress) bool
		exp      []sdk.AccAddress
	}{
		"capture all": {
			callback: func(addr sdk.AccAddress) bool {
				captured = append(captured, addr)
				return false
			},
			exp: []sdk.AccAddress{addr1, addr2, addr3},
		},
		"capture first": {
			callback: func(addr sdk.AccAddress) bool {
				captured = append(captured, addr)
				return true
			},
			exp: []sdk.AccAddress{addr1},
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			captured = nil
			// when
			k.IteratePrivileged(ctx, spec.callback)
			assert.Equal(t, spec.exp, captured)
		})
	}
}

func TestAppendToPrivilegedContracts(t *testing.T) {
	var (
		addr1 = sdk.AccAddress(bytes.Repeat([]byte{1}, address.Len))
		addr2 = sdk.AccAddress(bytes.Repeat([]byte{2}, address.Len))
		addr3 = sdk.AccAddress(bytes.Repeat([]byte{3}, address.Len))
	)

	type tuple struct {
		a sdk.AccAddress
		p uint8
	}

	specs := map[string]struct {
		setup        func(sdk.Context, *Keeper)
		srcType      types.PrivilegeType
		expPos       uint8
		expPersisted []tuple
		expErr       *sdkerrors.Error
	}{
		"first privilege": {
			setup:        func(ctx sdk.Context, k *Keeper) {},
			srcType:      types.PrivilegeTypeBeginBlock,
			expPos:       1,
			expPersisted: []tuple{{p: 1, a: addr1}},
		},
		"second privilege - ordered by position": {
			setup: func(ctx sdk.Context, k *Keeper) {
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeBeginBlock, addr3)
			},
			srcType:      types.PrivilegeTypeBeginBlock,
			expPos:       2,
			expPersisted: []tuple{{p: 1, a: addr3}, {p: 2, a: addr1}},
		},
		"second privilege with same address": {
			setup: func(ctx sdk.Context, k *Keeper) {
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeBeginBlock, addr1)
			},
			srcType:      types.PrivilegeTypeBeginBlock,
			expPos:       2,
			expPersisted: []tuple{{p: 1, a: addr1}, {p: 2, a: addr1}},
		},
		"other privilege type - separate group": {
			setup: func(ctx sdk.Context, k *Keeper) {
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeEndBlock, addr2)
			},
			srcType:      types.PrivilegeTypeBeginBlock,
			expPos:       1,
			expPersisted: []tuple{{p: 1, a: addr1}},
		},
		"singleton type fails when other exists": {
			setup: func(ctx sdk.Context, k *Keeper) {
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeValidatorSetUpdate, addr1)
			},
			srcType:      types.PrivilegeTypeValidatorSetUpdate,
			expPersisted: []tuple{{p: 1, a: addr1}},
			expPos:       0,
			expErr:       wasmtypes.ErrDuplicate,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			ctx, keepers := CreateDefaultTestInput(t, wasmkeeper.WithWasmEngine(NewWasmVMMock()))
			k := keepers.TWasmKeeper
			spec.setup(ctx, k)
			// when
			gotPos, gotErr := k.appendToPrivilegedContracts(ctx, spec.srcType, addr1)
			assert.True(t, spec.expErr.Is(gotErr), "expected %v but got #%+v", spec.expErr, gotErr)
			// then
			assert.Equal(t, spec.expPos, gotPos)
			var captured []tuple
			k.IteratePrivilegedContractsByType(ctx, spec.srcType, func(prio uint8, contractAddr sdk.AccAddress) bool {
				captured = append(captured, tuple{p: prio, a: contractAddr})
				return false
			})
			assert.Equal(t, spec.expPersisted, captured)
		})
	}
}

func TestRemovePrivilegedContractRegistration(t *testing.T) {
	var (
		myAddr      = sdk.AccAddress(bytes.Repeat([]byte{1}, address.Len))
		otherAddr   = sdk.AccAddress(bytes.Repeat([]byte{2}, address.Len))
		anotheraddr = sdk.AccAddress(bytes.Repeat([]byte{3}, address.Len))
	)

	type tuple struct {
		a sdk.AccAddress
		p uint8
	}

	specs := map[string]struct {
		setup        func(sdk.Context, *Keeper)
		srcPos       uint8
		expRemoved   bool
		expRemaining []tuple
	}{
		"one privilege": {
			setup: func(ctx sdk.Context, k *Keeper) {
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeBeginBlock, myAddr)
			},
			srcPos:     1,
			expRemoved: true,
		},
		"multiple privilege - first": {
			setup: func(ctx sdk.Context, k *Keeper) {
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeBeginBlock, myAddr)
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeBeginBlock, myAddr)
			},
			srcPos:       1,
			expRemoved:   true,
			expRemaining: []tuple{{p: 2, a: myAddr}},
		},
		"multiple privilege - middle": {
			setup: func(ctx sdk.Context, k *Keeper) {
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeBeginBlock, otherAddr)
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeBeginBlock, myAddr)
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeBeginBlock, anotheraddr)
			},
			srcPos:       2,
			expRemoved:   true,
			expRemaining: []tuple{{p: 1, a: otherAddr}, {p: 3, a: anotheraddr}},
		},
		"non existing position": {
			setup: func(ctx sdk.Context, k *Keeper) {
				k.appendToPrivilegedContracts(ctx, types.PrivilegeTypeBeginBlock, myAddr)
			},
			srcPos:       2,
			expRemoved:   false,
			expRemaining: []tuple{{p: 1, a: myAddr}},
		},
		"no privileges": {
			setup:      func(ctx sdk.Context, k *Keeper) {},
			srcPos:     1,
			expRemoved: false,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			ctx, keepers := CreateDefaultTestInput(t, wasmkeeper.WithWasmEngine(NewWasmVMMock()))
			k := keepers.TWasmKeeper
			spec.setup(ctx, k)

			// when
			removed := k.removePrivilegeRegistration(ctx, types.PrivilegeTypeBeginBlock, spec.srcPos, myAddr)

			// then
			var captured []tuple
			k.IteratePrivilegedContractsByType(ctx, types.PrivilegeTypeBeginBlock, func(prio uint8, contractAddr sdk.AccAddress) bool {
				captured = append(captured, tuple{p: prio, a: contractAddr})
				return false
			})
			assert.Equal(t, spec.expRemaining, captured)
			assert.Equal(t, spec.expRemoved, removed)
		})
	}
}

func seedTestContract(t *testing.T, ctx sdk.Context, k *Keeper) (uint64, sdk.AccAddress) {
	t.Helper()
	creatorAddr := rand.Bytes(address.Len)
	codeID, _, err := k.contractKeeper.Create(ctx, creatorAddr, []byte{}, nil)
	require.NoError(t, err)
	contractAddr, _, err := k.contractKeeper.Instantiate(ctx, codeID, creatorAddr, creatorAddr, nil, "", nil)
	require.NoError(t, err)
	return codeID, contractAddr
}
