package keeper

import (
	"fmt"
	"testing"

	proposaltypes "github.com/cosmos/cosmos-sdk/x/params/types/proposal"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/oldfurya/furya/x/twasm/contract"
	"github.com/oldfurya/furya/x/twasm/types"
)

func TestPetriHandlesDispatchMsg(t *testing.T) {
	var (
		contractAddr = RandomAddress(t)
		otherAddr    = RandomAddress(t)
	)
	specs := map[string]struct {
		setup                 func(m *handlerPetriKeeperMock)
		src                   wasmvmtypes.CosmosMsg
		expErr                *sdkerrors.Error
		expCapturedGovContent []govtypes.Content
		expEvents             []sdk.Event
	}{
		"handle privilege msg": {
			src: wasmvmtypes.CosmosMsg{
				Custom: []byte(`{"privilege":{"request":"begin_blocker"}}`),
			},
			setup: func(m *handlerPetriKeeperMock) {
				setupHandlerKeeperMock(m)
				m.GetContractInfoFn = emitCtxEventWithGetContractInfoFn(m.GetContractInfoFn, sdk.NewEvent("testing"))
			},
			expEvents: sdk.Events{sdk.NewEvent("testing")},
		},
		"handle execute gov proposal msg": {
			src: wasmvmtypes.CosmosMsg{
				Custom: []byte(`{"execute_gov_proposal":{"title":"foo", "description":"bar", "proposal":{"text":{}}}}`),
			},
			setup: func(m *handlerPetriKeeperMock) {
				setupHandlerKeeperMock(m, withPrivilegeSet(t, types.PrivilegeTypeGovProposalExecutor))
				m.GetContractInfoFn = emitCtxEventWithGetContractInfoFn(m.GetContractInfoFn, sdk.NewEvent("testing"))
			},
			expCapturedGovContent: []govtypes.Content{&govtypes.TextProposal{Title: "foo", Description: "bar"}},
			expEvents:             sdk.Events{sdk.NewEvent("testing")},
		},
		"handle mint msg": {
			src: wasmvmtypes.CosmosMsg{
				Custom: []byte(fmt.Sprintf(`{"mint_tokens":{"amount":"1","denom":"ufury","recipient":%q}}`, otherAddr.String())),
			},
			setup: func(m *handlerPetriKeeperMock) {
				setupHandlerKeeperMock(m, withPrivilegeSet(t, types.PrivilegeTypeTokenMinter))
			},
			expEvents: sdk.Events{sdk.NewEvent(
				types.EventTypeMintTokens,
				sdk.NewAttribute(wasmtypes.AttributeKeyContractAddr, contractAddr.String()),
				sdk.NewAttribute(sdk.AttributeKeyAmount, "1ufury"),
				sdk.NewAttribute(types.AttributeKeyRecipient, otherAddr.String()),
			)},
		},
		"handle consensus params change msg": {
			src: wasmvmtypes.CosmosMsg{
				Custom: []byte(`{"consensus_params":{"block":{"max_gas":100000000}}}`),
			},
			setup: func(m *handlerPetriKeeperMock) {
				setupHandlerKeeperMock(m, withPrivilegeSet(t, types.PrivilegeConsensusParamChanger))
			},
		},
		"handle delegate msg": {
			src: wasmvmtypes.CosmosMsg{
				Custom: []byte(fmt.Sprintf(`{"delegate":{ "funds": { "amount": "1", "denom": "ufury"} ,"staker":%q}}`, otherAddr.String())),
			},
			setup: func(m *handlerPetriKeeperMock) {
				setupHandlerKeeperMock(m, withPrivilegeSet(t, types.PrivilegeDelegator))
			},
			expEvents: sdk.Events{sdk.NewEvent(
				types.EventTypeDelegateTokens,
				sdk.NewAttribute(wasmtypes.AttributeKeyContractAddr, contractAddr.String()),
				sdk.NewAttribute(sdk.AttributeKeyAmount, "1ufury"),
				sdk.NewAttribute(types.AttributeKeySender, otherAddr.String()),
			)},
		},
		"handle undelegate msg": {
			src: wasmvmtypes.CosmosMsg{
				Custom: []byte(fmt.Sprintf(`{"undelegate":{ "funds": { "amount": "2", "denom": "ufury"} ,"recipient":%q}}`, otherAddr.String())),
			},
			setup: func(m *handlerPetriKeeperMock) {
				setupHandlerKeeperMock(m, withPrivilegeSet(t, types.PrivilegeDelegator))
			},
			expEvents: sdk.Events{sdk.NewEvent(
				types.EventTypeUndelegateTokens,
				sdk.NewAttribute(wasmtypes.AttributeKeyContractAddr, contractAddr.String()),
				sdk.NewAttribute(sdk.AttributeKeyAmount, "2ufury"),
				sdk.NewAttribute(types.AttributeKeyRecipient, otherAddr.String()),
			)},
		},
		"non custom msg rejected": {
			src:    wasmvmtypes.CosmosMsg{},
			setup:  func(m *handlerPetriKeeperMock) {},
			expErr: wasmtypes.ErrUnknownMsg,
		},
		"non privileged contracts rejected": {
			src: wasmvmtypes.CosmosMsg{Custom: []byte(`{}`)},
			setup: func(m *handlerPetriKeeperMock) {
				m.IsPrivilegedFn = func(ctx sdk.Context, contract sdk.AccAddress) bool {
					return false
				}
			},
			expErr: wasmtypes.ErrUnknownMsg,
		},
		"invalid json rejected": {
			src: wasmvmtypes.CosmosMsg{Custom: []byte(`not json`)},
			setup: func(m *handlerPetriKeeperMock) {
				m.IsPrivilegedFn = func(ctx sdk.Context, contract sdk.AccAddress) bool {
					return true
				}
			},
			expErr: sdkerrors.ErrJSONUnmarshal,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			cdc := MakeEncodingConfig(t).Codec
			govRouter := &CapturingGovRouter{}
			bankMock := NoopBankMock()
			mock := handlerPetriKeeperMock{}
			consensusStoreMock := NoopConsensusParamsStoreMock()
			spec.setup(&mock)
			h := NewPetriHandler(cdc, mock, bankMock, consensusStoreMock, govRouter)
			em := sdk.NewEventManager()
			ctx := sdk.Context{}.WithEventManager(em)

			// when
			gotEvents, _, gotErr := h.DispatchMsg(ctx, contractAddr, "", spec.src)
			// then
			require.True(t, spec.expErr.Is(gotErr), "expected %v but got %#+v", spec.expErr, gotErr)
			assert.Equal(t, spec.expCapturedGovContent, govRouter.captured)
			assert.Equal(t, spec.expEvents, gotEvents)
			assert.Empty(t, em.Events())
		})
	}
}

func withPrivilegeSet(t *testing.T, p types.PrivilegeType) func(info *wasmtypes.ContractInfo) {
	return func(info *wasmtypes.ContractInfo) {
		var details types.PetriContractDetails
		require.NoError(t, info.ReadExtension(&details))
		details.AddRegisteredPrivilege(p, 1)
		require.NoError(t, info.SetExtension(&details))
	}
}

func emitCtxEventWithGetContractInfoFn(fn func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo, event sdk.Event) func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
	return func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
		ctx.EventManager().EmitEvent(event)
		return fn(ctx, contractAddress)
	}
}

type registration struct {
	cb   types.PrivilegeType
	addr sdk.AccAddress
}
type unregistration struct {
	cb   types.PrivilegeType
	pos  uint8
	addr sdk.AccAddress
}

func TestPetriHandlesPrivilegeMsg(t *testing.T) {
	myContractAddr := RandomAddress(t)

	var capturedDetails *types.PetriContractDetails
	captureContractDetails := func(ctx sdk.Context, contract sdk.AccAddress, details *types.PetriContractDetails) error {
		require.Equal(t, myContractAddr, contract)
		capturedDetails = details
		return nil
	}

	var capturedRegistrations []registration
	captureRegistrations := func(ctx sdk.Context, privilegeType types.PrivilegeType, contractAddress sdk.AccAddress) (uint8, error) {
		capturedRegistrations = append(capturedRegistrations, registration{cb: privilegeType, addr: contractAddress})
		return 1, nil
	}
	var capturedUnRegistrations []unregistration
	captureUnRegistrations := func(ctx sdk.Context, privilegeType types.PrivilegeType, pos uint8, contractAddress sdk.AccAddress) bool {
		capturedUnRegistrations = append(capturedUnRegistrations, unregistration{cb: privilegeType, pos: pos, addr: contractAddress})
		return true
	}

	captureWithMock := func(mutators ...func(*wasmtypes.ContractInfo)) func(mock *handlerPetriKeeperMock) {
		return func(m *handlerPetriKeeperMock) {
			m.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
				f := wasmtypes.ContractInfoFixture(mutators...)
				return &f
			}
			m.setContractDetailsFn = captureContractDetails
			m.appendToPrivilegedContractsFn = captureRegistrations
			m.removePrivilegeRegistrationFn = captureUnRegistrations
		}
	}

	specs := map[string]struct {
		setup              func(m *handlerPetriKeeperMock)
		src                contract.PrivilegeMsg
		expDetails         *types.PetriContractDetails
		expRegistrations   []registration
		expUnRegistrations []unregistration
		expErr             *sdkerrors.Error
	}{
		"register begin block": {
			src:   contract.PrivilegeMsg{Request: types.PrivilegeTypeBeginBlock},
			setup: captureWithMock(),
			expDetails: &types.PetriContractDetails{
				RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "begin_blocker"}},
			},
			expRegistrations: []registration{{cb: types.PrivilegeTypeBeginBlock, addr: myContractAddr}},
		},
		"unregister begin block": {
			src: contract.PrivilegeMsg{Release: types.PrivilegeTypeBeginBlock},
			setup: captureWithMock(func(info *wasmtypes.ContractInfo) {
				ext := &types.PetriContractDetails{
					RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "begin_blocker"}},
				}
				info.SetExtension(ext)
			}),
			expDetails:         &types.PetriContractDetails{RegisteredPrivileges: []types.RegisteredPrivilege{}},
			expUnRegistrations: []unregistration{{cb: types.PrivilegeTypeBeginBlock, pos: 1, addr: myContractAddr}},
		},
		"register end block": {
			src:   contract.PrivilegeMsg{Request: types.PrivilegeTypeEndBlock},
			setup: captureWithMock(),
			expDetails: &types.PetriContractDetails{
				RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "end_blocker"}},
			},
			expRegistrations: []registration{{cb: types.PrivilegeTypeEndBlock, addr: myContractAddr}},
		},
		"unregister end block": {
			src: contract.PrivilegeMsg{Release: types.PrivilegeTypeEndBlock},
			setup: captureWithMock(func(info *wasmtypes.ContractInfo) {
				ext := &types.PetriContractDetails{
					RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "end_blocker"}},
				}
				info.SetExtension(ext)
			}),
			expDetails:         &types.PetriContractDetails{RegisteredPrivileges: []types.RegisteredPrivilege{}},
			expUnRegistrations: []unregistration{{cb: types.PrivilegeTypeEndBlock, pos: 1, addr: myContractAddr}},
		},
		"register validator set update block": {
			src:   contract.PrivilegeMsg{Request: types.PrivilegeTypeValidatorSetUpdate},
			setup: captureWithMock(),
			expDetails: &types.PetriContractDetails{
				RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "validator_set_updater"}},
			},
			expRegistrations: []registration{{cb: types.PrivilegeTypeValidatorSetUpdate, addr: myContractAddr}},
		},
		"unregister validator set update block": {
			src: contract.PrivilegeMsg{Release: types.PrivilegeTypeValidatorSetUpdate},
			setup: captureWithMock(func(info *wasmtypes.ContractInfo) {
				ext := &types.PetriContractDetails{
					RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "validator_set_updater"}},
				}
				info.SetExtension(ext)
			}),
			expDetails:         &types.PetriContractDetails{RegisteredPrivileges: []types.RegisteredPrivilege{}},
			expUnRegistrations: []unregistration{{cb: types.PrivilegeTypeValidatorSetUpdate, pos: 1, addr: myContractAddr}},
		},
		"register gov proposal executor": {
			src:   contract.PrivilegeMsg{Request: types.PrivilegeTypeGovProposalExecutor},
			setup: captureWithMock(),
			expDetails: &types.PetriContractDetails{
				RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "gov_proposal_executor"}},
			},
			expRegistrations: []registration{{cb: types.PrivilegeTypeGovProposalExecutor, addr: myContractAddr}},
		},
		"unregister gov proposal executor": {
			src: contract.PrivilegeMsg{Release: types.PrivilegeTypeGovProposalExecutor},
			setup: captureWithMock(func(info *wasmtypes.ContractInfo) {
				ext := &types.PetriContractDetails{
					RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "gov_proposal_executor"}},
				}
				info.SetExtension(ext)
			}),
			expDetails:         &types.PetriContractDetails{RegisteredPrivileges: []types.RegisteredPrivilege{}},
			expUnRegistrations: []unregistration{{cb: types.PrivilegeTypeGovProposalExecutor, pos: 1, addr: myContractAddr}},
		},
		"register delegator": {
			src:   contract.PrivilegeMsg{Request: types.PrivilegeDelegator},
			setup: captureWithMock(),
			expDetails: &types.PetriContractDetails{
				RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "delegator"}},
			},
			expRegistrations: []registration{{cb: types.PrivilegeDelegator, addr: myContractAddr}},
		},
		"unregister delegator": {
			src: contract.PrivilegeMsg{Release: types.PrivilegeDelegator},
			setup: captureWithMock(func(info *wasmtypes.ContractInfo) {
				ext := &types.PetriContractDetails{
					RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "delegator"}},
				}
				info.SetExtension(ext)
			}),
			expDetails:         &types.PetriContractDetails{RegisteredPrivileges: []types.RegisteredPrivilege{}},
			expUnRegistrations: []unregistration{{cb: types.PrivilegeDelegator, pos: 1, addr: myContractAddr}},
		},
		"register privilege fails": {
			src: contract.PrivilegeMsg{Request: types.PrivilegeTypeValidatorSetUpdate},
			setup: func(m *handlerPetriKeeperMock) {
				m.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					r := wasmtypes.ContractInfoFixture()
					return &r
				}
				m.appendToPrivilegedContractsFn = func(ctx sdk.Context, privilegeType types.PrivilegeType, contractAddress sdk.AccAddress) (uint8, error) {
					return 0, wasmtypes.ErrDuplicate
				}
			},
			expErr: wasmtypes.ErrDuplicate,
		},
		"register begin block with existing registration": {
			src: contract.PrivilegeMsg{Request: types.PrivilegeTypeBeginBlock},
			setup: captureWithMock(func(info *wasmtypes.ContractInfo) {
				info.SetExtension(&types.PetriContractDetails{
					RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "begin_blocker"}},
				})
			}),
		},
		"register appends to existing callback list": {
			src: contract.PrivilegeMsg{Request: types.PrivilegeTypeBeginBlock},
			setup: captureWithMock(func(info *wasmtypes.ContractInfo) {
				info.SetExtension(&types.PetriContractDetails{
					RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 100, PrivilegeType: "end_blocker"}},
				})
			}),
			expDetails: &types.PetriContractDetails{
				RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 100, PrivilegeType: "end_blocker"}, {Position: 1, PrivilegeType: "begin_blocker"}},
			},
			expRegistrations: []registration{{cb: types.PrivilegeTypeBeginBlock, addr: myContractAddr}},
		},
		"unregister removed from existing callback list": {
			src: contract.PrivilegeMsg{Release: types.PrivilegeTypeBeginBlock},
			setup: captureWithMock(func(info *wasmtypes.ContractInfo) {
				ext := &types.PetriContractDetails{
					RegisteredPrivileges: []types.RegisteredPrivilege{
						{Position: 3, PrivilegeType: "validator_set_updater"},
						{Position: 1, PrivilegeType: "begin_blocker"},
						{Position: 100, PrivilegeType: "end_blocker"},
					},
				}
				info.SetExtension(ext)
			}),
			expDetails: &types.PetriContractDetails{RegisteredPrivileges: []types.RegisteredPrivilege{
				{Position: 3, PrivilegeType: "validator_set_updater"},
				{Position: 100, PrivilegeType: "end_blocker"},
			}},
			expUnRegistrations: []unregistration{{cb: types.PrivilegeTypeBeginBlock, pos: 1, addr: myContractAddr}},
		},
		"unregister begin block without existing registration": {
			src:   contract.PrivilegeMsg{Release: types.PrivilegeTypeBeginBlock},
			setup: captureWithMock(),
		},
		"empty privilege msg rejected": {
			setup: func(m *handlerPetriKeeperMock) {
				setupHandlerKeeperMock(m)
			},
			expErr: wasmtypes.ErrUnknownMsg,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			capturedDetails, capturedRegistrations, capturedUnRegistrations = nil, nil, nil
			mock := handlerPetriKeeperMock{}
			spec.setup(&mock)
			h := NewPetriHandler(nil, mock, nil, nil, nil)
			var ctx sdk.Context
			gotErr := h.handlePrivilege(ctx, myContractAddr, &spec.src)
			require.True(t, spec.expErr.Is(gotErr), "expected %v but got %#+v", spec.expErr, gotErr)
			if spec.expErr != nil {
				return
			}
			assert.Equal(t, spec.expDetails, capturedDetails)
			assert.Equal(t, spec.expRegistrations, capturedRegistrations)
			assert.Equal(t, spec.expUnRegistrations, capturedUnRegistrations)
		})
	}
}

func TestHandleGovProposalExecution(t *testing.T) {
	myContractAddr := RandomAddress(t)
	specs := map[string]struct {
		src                   contract.ExecuteGovProposal
		setup                 func(m *handlerPetriKeeperMock)
		expErr                *sdkerrors.Error
		expCapturedGovContent []govtypes.Content
	}{
		"all good": {
			src:                   contract.ExecuteGovProposalFixture(),
			setup:                 withPrivilegeRegistered(types.PrivilegeTypeGovProposalExecutor),
			expCapturedGovContent: []govtypes.Content{&govtypes.TextProposal{Title: "foo", Description: "bar"}},
		},
		"non consensus params accepted": {
			src: contract.ExecuteGovProposalFixture(func(p *contract.ExecuteGovProposal) {
				p.Proposal = contract.GovProposalFixture(func(x *contract.GovProposal) {
					x.ChangeParams = &[]proposaltypes.ParamChange{
						{Subspace: "foo", Key: "bar", Value: `{"example": "value"}`},
					}
				})
			}),
			setup: withPrivilegeRegistered(types.PrivilegeTypeGovProposalExecutor),
			expCapturedGovContent: []govtypes.Content{&proposaltypes.ParameterChangeProposal{
				Title:       "foo",
				Description: "bar",
				Changes: []proposaltypes.ParamChange{
					{Subspace: "foo", Key: "bar", Value: `{"example": "value"}`},
				},
			}},
		},
		"unauthorized contract": {
			src: contract.ExecuteGovProposalFixture(),
			setup: func(m *handlerPetriKeeperMock) {
				m.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					c := wasmtypes.ContractInfoFixture()
					return &c
				}
			},
			expErr: sdkerrors.ErrUnauthorized,
		},
		"invalid content": {
			src: contract.ExecuteGovProposalFixture(func(p *contract.ExecuteGovProposal) {
				p.Proposal = contract.GovProposalFixture(func(x *contract.GovProposal) {
					x.RegisterUpgrade = &upgradetypes.Plan{}
				})
			}),
			setup:  withPrivilegeRegistered(types.PrivilegeTypeGovProposalExecutor),
			expErr: sdkerrors.ErrInvalidRequest,
		},
		"no content": {
			src:    contract.ExecuteGovProposal{Title: "foo", Description: "bar"},
			setup:  withPrivilegeRegistered(types.PrivilegeTypeGovProposalExecutor),
			expErr: wasmtypes.ErrUnknownMsg,
		},
		"unknown origin contract": {
			src: contract.ExecuteGovProposalFixture(),
			setup: func(m *handlerPetriKeeperMock) {
				m.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					return nil
				}
			},
			expErr: wasmtypes.ErrNotFound,
		},
		"consensus params rejected": {
			src: contract.ExecuteGovProposalFixture(func(p *contract.ExecuteGovProposal) {
				p.Proposal = contract.GovProposalFixture(func(x *contract.GovProposal) {
					x.ChangeParams = &[]proposaltypes.ParamChange{
						{
							Subspace: "baseapp",
							Key:      "BlockParams",
							Value:    `{"max_bytes": "1"}`,
						},
					}
				})
			}),
			setup:  withPrivilegeRegistered(types.PrivilegeTypeGovProposalExecutor),
			expErr: sdkerrors.ErrUnauthorized,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			cdc := MakeEncodingConfig(t).Codec
			mock := handlerPetriKeeperMock{}
			spec.setup(&mock)
			router := &CapturingGovRouter{}
			h := NewPetriHandler(cdc, mock, nil, nil, router)
			var ctx sdk.Context
			gotErr := h.handleGovProposalExecution(ctx, myContractAddr, &spec.src)
			require.True(t, spec.expErr.Is(gotErr), "expected %v but got %#+v", spec.expErr, gotErr)
			assert.Equal(t, spec.expCapturedGovContent, router.captured)
		})
	}
}

func TestHandleMintToken(t *testing.T) {
	myContractAddr := RandomAddress(t)
	myRecipientAddr := RandomAddress(t)
	specs := map[string]struct {
		src            contract.MintTokens
		setup          func(k *handlerPetriKeeperMock)
		expErr         *sdkerrors.Error
		expMintedCoins sdk.Coins
		expRecipient   sdk.AccAddress
	}{
		"all good": {
			src: contract.MintTokens{
				Denom:         "foo",
				Amount:        "123",
				RecipientAddr: myRecipientAddr.String(),
			},
			setup:          withPrivilegeRegistered(types.PrivilegeTypeTokenMinter),
			expMintedCoins: sdk.NewCoins(sdk.NewCoin("foo", sdk.NewInt(123))),
			expRecipient:   myRecipientAddr,
		},
		"unauthorized contract": {
			src: contract.MintTokens{
				Denom:         "foo",
				Amount:        "123",
				RecipientAddr: myRecipientAddr.String(),
			},
			setup: func(k *handlerPetriKeeperMock) {
				k.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					c := wasmtypes.ContractInfoFixture(func(info *wasmtypes.ContractInfo) {
						info.SetExtension(&types.PetriContractDetails{
							RegisteredPrivileges: []types.RegisteredPrivilege{},
						})
					})
					return &c
				}
			},
			expErr: sdkerrors.ErrUnauthorized,
		},
		"invalid denom": {
			src: contract.MintTokens{
				Denom:         "&&&foo",
				Amount:        "123",
				RecipientAddr: myRecipientAddr.String(),
			},
			setup:  withPrivilegeRegistered(types.PrivilegeTypeTokenMinter),
			expErr: sdkerrors.ErrInvalidCoins,
		},
		"invalid amount": {
			src: contract.MintTokens{
				Denom:         "foo",
				Amount:        "not-a-number",
				RecipientAddr: myRecipientAddr.String(),
			},
			setup:  withPrivilegeRegistered(types.PrivilegeTypeTokenMinter),
			expErr: sdkerrors.ErrInvalidCoins,
		},
		"invalid recipient": {
			src: contract.MintTokens{
				Denom:         "foo",
				Amount:        "123",
				RecipientAddr: "not-an-address",
			},
			setup: func(k *handlerPetriKeeperMock) {
				k.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					c := wasmtypes.ContractInfoFixture(func(info *wasmtypes.ContractInfo) {
						info.SetExtension(&types.PetriContractDetails{
							RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "token_minter"}},
						})
					})
					return &c
				}
			},
			expErr: sdkerrors.ErrInvalidAddress,
		},
		"no content": {
			src:    contract.MintTokens{},
			setup:  withPrivilegeRegistered(types.PrivilegeTypeTokenMinter),
			expErr: sdkerrors.ErrInvalidAddress,
		},
		"unknown origin contract": {
			src: contract.MintTokens{
				Denom:         "foo",
				Amount:        "123",
				RecipientAddr: "not-an-address",
			},
			setup: func(m *handlerPetriKeeperMock) {
				m.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					return nil
				}
			},
			expErr: wasmtypes.ErrNotFound,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			cdc := MakeEncodingConfig(t).Codec
			mintFn, capturedMintedCoins := CaptureMintedCoinsFn()
			sendFn, capturedSentCoins := CaptureSentCoinsFromModuleFn()
			mock := BankMock{MintCoinsFn: mintFn, SendCoinsFromModuleToAccountFn: sendFn}
			keeperMock := handlerPetriKeeperMock{}
			spec.setup(&keeperMock)
			h := NewPetriHandler(cdc, keeperMock, mock, nil, nil)
			var ctx sdk.Context
			gotEvts, gotErr := h.handleMintToken(ctx, myContractAddr, &spec.src)
			require.True(t, spec.expErr.Is(gotErr), "expected %v but got %#+v", spec.expErr, gotErr)
			if spec.expErr != nil {
				assert.Len(t, gotEvts, 0)
				return
			}
			require.Len(t, *capturedMintedCoins, 1)
			assert.Equal(t, spec.expMintedCoins, (*capturedMintedCoins)[0])
			require.Len(t, *capturedSentCoins, 1)
			assert.Equal(t, (*capturedSentCoins)[0].coins, spec.expMintedCoins)
			assert.Equal(t, (*capturedSentCoins)[0].recipientAddr, spec.expRecipient)
			require.Len(t, gotEvts, 1)
			assert.Equal(t, types.EventTypeMintTokens, gotEvts[0].Type)
		})
	}
}

func TestHandleConsensusParamsUpdate(t *testing.T) {
	var (
		myContractAddr = RandomAddress(t)
		// some integers
		one, two, three, four, five int64 = 1, 2, 3, 4, 5
	)
	specs := map[string]struct {
		src       contract.ConsensusParamsUpdate
		setup     func(k *handlerPetriKeeperMock)
		expErr    *sdkerrors.Error
		expStored *abci.ConsensusParams
	}{
		"all good": {
			src: contract.ConsensusParamsUpdate{
				Block: &contract.BlockParams{
					MaxBytes: &one,
					MaxGas:   &two,
				},
				Evidence: &contract.EvidenceParams{
					MaxAgeNumBlocks: &three,
					MaxAgeDuration:  &four,
					MaxBytes:        &five,
				},
			},
			setup: withPrivilegeRegistered(types.PrivilegeConsensusParamChanger),
			expStored: types.ConsensusParamsFixture(func(c *abci.ConsensusParams) {
				c.Block.MaxBytes = 1
				c.Block.MaxGas = 2
				c.Evidence.MaxAgeNumBlocks = 3
				c.Evidence.MaxAgeDuration = 4 * 1_000_000_000 // nanos
				c.Evidence.MaxBytes = 5
			}),
		},
		"unauthorized": {
			src: contract.ConsensusParamsUpdate{
				Evidence: &contract.EvidenceParams{
					MaxAgeNumBlocks: &one,
				},
			},
			setup: func(k *handlerPetriKeeperMock) {
				k.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					c := wasmtypes.ContractInfoFixture()
					return &c
				}
			},
			expErr: sdkerrors.ErrUnauthorized,
		},
		"invalid msg": {
			src:    contract.ConsensusParamsUpdate{},
			setup:  withPrivilegeRegistered(types.PrivilegeConsensusParamChanger),
			expErr: wasmtypes.ErrEmpty,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			cdc := MakeEncodingConfig(t).Codec
			var gotStored *abci.ConsensusParams
			mock := ConsensusParamsStoreMock{
				GetConsensusParamsFn:   func(ctx sdk.Context) *abci.ConsensusParams { return types.ConsensusParamsFixture() },
				StoreConsensusParamsFn: func(ctx sdk.Context, cp *abci.ConsensusParams) { gotStored = cp },
			}

			keeperMock := handlerPetriKeeperMock{}
			spec.setup(&keeperMock)
			h := NewPetriHandler(cdc, keeperMock, nil, mock, nil)
			var ctx sdk.Context
			gotEvts, gotErr := h.handleConsensusParamsUpdate(ctx, myContractAddr, &spec.src)
			require.True(t, spec.expErr.Is(gotErr), "expected %v but got %#+v", spec.expErr, gotErr)
			assert.Len(t, gotEvts, 0)
			assert.Equal(t, spec.expStored, gotStored)
		})
	}
}

func TestHandleDelegate(t *testing.T) {
	myContractAddr := RandomAddress(t)
	myStakerAddr := RandomAddress(t)
	specs := map[string]struct {
		src               contract.Delegate
		setup             func(k *handlerPetriKeeperMock)
		expErr            *sdkerrors.Error
		expSentCoins      sdk.Coins
		expDelegatedCoins capturedSentCoinsFromAddress
	}{
		"all good": {
			src: contract.Delegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "foo",
					Amount: "123",
				},
				StakerAddr: myStakerAddr.String(),
			},
			setup:        withPrivilegeRegistered(types.PrivilegeDelegator),
			expSentCoins: sdk.NewCoins(sdk.NewCoin("foo", sdk.NewInt(123))),
			expDelegatedCoins: capturedSentCoinsFromAddress{
				recipientModule: "bonded_tokens_pool",
				coins:           sdk.NewCoins(sdk.NewCoin("foo", sdk.NewInt(123))),
			},
		},
		"unauthorized contract": {
			src: contract.Delegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "foo",
					Amount: "123",
				},
				StakerAddr: myStakerAddr.String(),
			},
			setup: func(k *handlerPetriKeeperMock) {
				k.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					c := wasmtypes.ContractInfoFixture(func(info *wasmtypes.ContractInfo) {
						info.SetExtension(&types.PetriContractDetails{
							RegisteredPrivileges: []types.RegisteredPrivilege{},
						})
					})
					return &c
				}
			},
			expErr: sdkerrors.ErrUnauthorized,
		},
		"invalid denom": {
			src: contract.Delegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "&&&foo",
					Amount: "123",
				},
				StakerAddr: myStakerAddr.String(),
			},
			setup:  withPrivilegeRegistered(types.PrivilegeDelegator),
			expErr: sdkerrors.ErrJSONUnmarshal,
		},
		"invalid amount": {
			src: contract.Delegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "foo",
					Amount: "not-a-number",
				},
				StakerAddr: myStakerAddr.String(),
			},
			setup:  withPrivilegeRegistered(types.PrivilegeDelegator),
			expErr: sdkerrors.ErrJSONUnmarshal,
		},
		"invalid recipient": {
			src: contract.Delegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "foo",
					Amount: "123",
				},
				StakerAddr: "not-an-address",
			},
			setup: func(k *handlerPetriKeeperMock) {
				k.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					c := wasmtypes.ContractInfoFixture(func(info *wasmtypes.ContractInfo) {
						info.SetExtension(&types.PetriContractDetails{
							RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "delegator"}},
						})
					})
					return &c
				}
			},
			expErr: sdkerrors.ErrInvalidAddress,
		},
		"no content": {
			src:    contract.Delegate{},
			setup:  withPrivilegeRegistered(types.PrivilegeDelegator),
			expErr: sdkerrors.ErrInvalidAddress,
		},
		"unknown origin contract": {
			src: contract.Delegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "&&&foo",
					Amount: "123",
				},
				StakerAddr: "not-an-address",
			},
			setup: func(m *handlerPetriKeeperMock) {
				m.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					return nil
				}
			},
			expErr: wasmtypes.ErrNotFound,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			cdc := MakeEncodingConfig(t).Codec
			delegateFn, capturedDelegatedCoins := CaptureDelegatedCoinsFn()
			sendFn, capturedSentCoins := CaptureSentCoinsFromModuleFn()
			mock := BankMock{DelegateCoinsFromAccountToModuleFn: delegateFn, SendCoinsFromModuleToAccountFn: sendFn}
			keeperMock := handlerPetriKeeperMock{}
			spec.setup(&keeperMock)
			h := NewPetriHandler(cdc, keeperMock, mock, nil, nil)
			var ctx sdk.Context
			gotEvts, gotErr := h.handleDelegate(ctx, myContractAddr, &spec.src)
			require.True(t, spec.expErr.Is(gotErr), "expected %v but got %#+v", spec.expErr, gotErr)
			if spec.expErr != nil {
				assert.Len(t, gotEvts, 0)
				return
			}
			require.Len(t, *capturedDelegatedCoins, 1)
			assert.Equal(t, spec.expDelegatedCoins, (*capturedDelegatedCoins)[0])
			require.Len(t, *capturedSentCoins, 1)
			assert.Equal(t, (*capturedSentCoins)[0].coins, spec.expSentCoins)
			assert.Equal(t, (*capturedSentCoins)[0].recipientAddr, myContractAddr)
			require.Len(t, gotEvts, 1)
			assert.Equal(t, types.EventTypeDelegateTokens, gotEvts[0].Type)
		})
	}
}

func TestHandleUndelegate(t *testing.T) {
	myContractAddr := RandomAddress(t)
	myRecipientAddr := RandomAddress(t)
	specs := map[string]struct {
		src                 contract.Undelegate
		setup               func(k *handlerPetriKeeperMock)
		expErr              *sdkerrors.Error
		expSentCoins        sdk.Coins
		expUndelegatedCoins capturedSentCoinsFromModule
	}{
		"all good": {
			src: contract.Undelegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "foo",
					Amount: "123",
				},
				RecipientAddr: myRecipientAddr.String(),
			},
			setup:        withPrivilegeRegistered(types.PrivilegeDelegator),
			expSentCoins: sdk.NewCoins(sdk.NewCoin("foo", sdk.NewInt(123))),
			expUndelegatedCoins: capturedSentCoinsFromModule{
				recipientAddr: myRecipientAddr,
				coins:         sdk.NewCoins(sdk.NewCoin("foo", sdk.NewInt(123))),
			},
		},
		"unauthorized contract": {
			src: contract.Undelegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "foo",
					Amount: "123",
				},
				RecipientAddr: myRecipientAddr.String(),
			},
			setup: func(k *handlerPetriKeeperMock) {
				k.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					c := wasmtypes.ContractInfoFixture(func(info *wasmtypes.ContractInfo) {
						info.SetExtension(&types.PetriContractDetails{
							RegisteredPrivileges: []types.RegisteredPrivilege{},
						})
					})
					return &c
				}
			},
			expErr: sdkerrors.ErrUnauthorized,
		},
		"invalid denom": {
			src: contract.Undelegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "&&&foo",
					Amount: "123",
				},
				RecipientAddr: myRecipientAddr.String(),
			},
			setup:  withPrivilegeRegistered(types.PrivilegeDelegator),
			expErr: sdkerrors.ErrJSONUnmarshal,
		},
		"invalid amount": {
			src: contract.Undelegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "foo",
					Amount: "not-a-number",
				},
				RecipientAddr: myRecipientAddr.String(),
			},
			setup:  withPrivilegeRegistered(types.PrivilegeDelegator),
			expErr: sdkerrors.ErrJSONUnmarshal,
		},
		"invalid recipient": {
			src: contract.Undelegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "foo",
					Amount: "123",
				},
				RecipientAddr: "not-an-address",
			},
			setup: func(k *handlerPetriKeeperMock) {
				k.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					c := wasmtypes.ContractInfoFixture(func(info *wasmtypes.ContractInfo) {
						info.SetExtension(&types.PetriContractDetails{
							RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: "delegator"}},
						})
					})
					return &c
				}
			},
			expErr: sdkerrors.ErrInvalidAddress,
		},
		"no content": {
			src:    contract.Undelegate{},
			setup:  withPrivilegeRegistered(types.PrivilegeDelegator),
			expErr: sdkerrors.ErrInvalidAddress,
		},
		"unknown origin contract": {
			src: contract.Undelegate{
				Funds: wasmvmtypes.Coin{
					Denom:  "&&&foo",
					Amount: "123",
				},
				RecipientAddr: "not-an-address",
			},
			setup: func(m *handlerPetriKeeperMock) {
				m.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
					return nil
				}
			},
			expErr: wasmtypes.ErrNotFound,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			cdc := MakeEncodingConfig(t).Codec
			undelegateFn, capturedUndelegatedCoins := CaptureUndelegatedCoinsFn()
			sendFn, capturedSentCoins := CaptureSentCoinsFromAccountFn()
			mock := BankMock{UndelegateCoinsFromModuleToAccountFn: undelegateFn, SendCoinsFromAccountToModuleFn: sendFn}
			keeperMock := handlerPetriKeeperMock{}
			spec.setup(&keeperMock)
			h := NewPetriHandler(cdc, keeperMock, mock, nil, nil)
			var ctx sdk.Context
			gotEvts, gotErr := h.handleUndelegate(ctx, myContractAddr, &spec.src)
			require.True(t, spec.expErr.Is(gotErr), "expected %v but got %#+v", spec.expErr, gotErr)
			if spec.expErr != nil {
				assert.Len(t, gotEvts, 0)
				return
			}
			require.Len(t, *capturedUndelegatedCoins, 1)
			assert.Equal(t, spec.expUndelegatedCoins, (*capturedUndelegatedCoins)[0])
			require.Len(t, *capturedSentCoins, 1)
			assert.Equal(t, (*capturedSentCoins)[0].coins, spec.expSentCoins)
			assert.Equal(t, (*capturedSentCoins)[0].recipientModule, "bonded_tokens_pool")
			require.Len(t, gotEvts, 1)
			assert.Equal(t, types.EventTypeUndelegateTokens, gotEvts[0].Type)
		})
	}
}

func withPrivilegeRegistered(p types.PrivilegeType) func(k *handlerPetriKeeperMock) {
	return func(k *handlerPetriKeeperMock) {
		k.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
			c := wasmtypes.ContractInfoFixture(func(info *wasmtypes.ContractInfo) {
				info.SetExtension(&types.PetriContractDetails{
					RegisteredPrivileges: []types.RegisteredPrivilege{{Position: 1, PrivilegeType: p.String()}},
				})
			})
			return &c
		}
	}
}

// setupHandlerKeeperMock provided method stubs for all methods for registration
func setupHandlerKeeperMock(m *handlerPetriKeeperMock, mutators ...func(*wasmtypes.ContractInfo)) {
	m.IsPrivilegedFn = func(ctx sdk.Context, contract sdk.AccAddress) bool {
		return true
	}
	m.GetContractInfoFn = func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
		v := wasmtypes.ContractInfoFixture(append([]func(*wasmtypes.ContractInfo){func(info *wasmtypes.ContractInfo) {
			info.SetExtension(&types.PetriContractDetails{})
		}}, mutators...)...)
		return &v
	}
	m.appendToPrivilegedContractsFn = func(ctx sdk.Context, privilegeType types.PrivilegeType, contractAddress sdk.AccAddress) (uint8, error) {
		return 1, nil
	}
	m.setContractDetailsFn = func(ctx sdk.Context, contract sdk.AccAddress, details *types.PetriContractDetails) error {
		return nil
	}
}

var _ PetriWasmHandlerKeeper = handlerPetriKeeperMock{}

type handlerPetriKeeperMock struct {
	IsPrivilegedFn                func(ctx sdk.Context, contract sdk.AccAddress) bool
	appendToPrivilegedContractsFn func(ctx sdk.Context, privilegeType types.PrivilegeType, contractAddress sdk.AccAddress) (uint8, error)
	removePrivilegeRegistrationFn func(ctx sdk.Context, privilegeType types.PrivilegeType, pos uint8, contractAddr sdk.AccAddress) bool
	setContractDetailsFn          func(ctx sdk.Context, contract sdk.AccAddress, details *types.PetriContractDetails) error
	GetContractInfoFn             func(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo
}

func (m handlerPetriKeeperMock) IsPrivileged(ctx sdk.Context, contract sdk.AccAddress) bool {
	if m.IsPrivilegedFn == nil {
		panic("not expected to be called")
	}
	return m.IsPrivilegedFn(ctx, contract)
}

func (m handlerPetriKeeperMock) appendToPrivilegedContracts(ctx sdk.Context, privilegeType types.PrivilegeType, contractAddress sdk.AccAddress) (uint8, error) {
	if m.appendToPrivilegedContractsFn == nil {
		panic("not expected to be called")
	}
	return m.appendToPrivilegedContractsFn(ctx, privilegeType, contractAddress)
}

func (m handlerPetriKeeperMock) removePrivilegeRegistration(ctx sdk.Context, privilegeType types.PrivilegeType, pos uint8, contractAddr sdk.AccAddress) bool {
	if m.removePrivilegeRegistrationFn == nil {
		panic("not expected to be called")
	}
	return m.removePrivilegeRegistrationFn(ctx, privilegeType, pos, contractAddr)
}

func (m handlerPetriKeeperMock) setContractDetails(ctx sdk.Context, contract sdk.AccAddress, details *types.PetriContractDetails) error {
	if m.setContractDetailsFn == nil {
		panic("not expected to be called")
	}
	return m.setContractDetailsFn(ctx, contract, details)
}

func (m handlerPetriKeeperMock) GetContractInfo(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo {
	if m.GetContractInfoFn == nil {
		panic("not expected to be called")
	}
	return m.GetContractInfoFn(ctx, contractAddress)
}

// BankMock test helper that satisfies the `bankKeeper` interface
type BankMock struct {
	MintCoinsFn                          func(ctx sdk.Context, moduleName string, amt sdk.Coins) error
	SendCoinsFromModuleToAccountFn       func(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModuleFn       func(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	DelegateCoinsFromAccountToModuleFn   func(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	UndelegateCoinsFromModuleToAccountFn func(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

func (m BankMock) MintCoins(ctx sdk.Context, moduleName string, amt sdk.Coins) error {
	if m.MintCoinsFn == nil {
		panic("not expected to be called")
	}
	return m.MintCoinsFn(ctx, moduleName, amt)
}

func (m BankMock) SendCoinsFromModuleToAccount(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.SendCoinsFromModuleToAccountFn == nil {
		panic("not expected to be called")
	}
	return m.SendCoinsFromModuleToAccountFn(ctx, senderModule, recipientAddr, amt)
}

func (m BankMock) SendCoinsFromAccountToModule(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.SendCoinsFromAccountToModuleFn == nil {
		panic("not expected to be called")
	}
	return m.SendCoinsFromAccountToModuleFn(ctx, senderAddr, recipientModule, amt)
}

func (m BankMock) UndelegateCoinsFromModuleToAccount(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.UndelegateCoinsFromModuleToAccountFn == nil {
		panic("not expected to be called")
	}
	return m.UndelegateCoinsFromModuleToAccountFn(ctx, senderModule, recipientAddr, amt)
}

func (m BankMock) DelegateCoinsFromAccountToModule(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.DelegateCoinsFromAccountToModuleFn == nil {
		panic("not expected to be called")
	}
	return m.DelegateCoinsFromAccountToModuleFn(ctx, senderAddr, recipientModule, amt)
}

func NoopBankMock() *BankMock {
	return &BankMock{
		MintCoinsFn: func(ctx sdk.Context, moduleName string, amt sdk.Coins) error {
			return nil
		},
		SendCoinsFromModuleToAccountFn: func(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
			return nil
		},
		SendCoinsFromAccountToModuleFn: func(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
			return nil
		},
		DelegateCoinsFromAccountToModuleFn: func(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
			return nil
		},
		UndelegateCoinsFromModuleToAccountFn: func(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
			return nil
		},
	}
}

func CaptureMintedCoinsFn() (func(ctx sdk.Context, moduleName string, amt sdk.Coins) error, *[]sdk.Coins) {
	var r []sdk.Coins
	return func(ctx sdk.Context, moduleName string, amt sdk.Coins) error {
		r = append(r, amt)
		return nil
	}, &r
}

type capturedSentCoinsFromModule struct {
	recipientAddr sdk.AccAddress
	coins         sdk.Coins
}

type capturedSentCoinsFromAddress struct {
	recipientModule string
	coins           sdk.Coins
}

func CaptureSentCoinsFromModuleFn() (func(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error, *[]capturedSentCoinsFromModule) {
	var r []capturedSentCoinsFromModule
	return func(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
		r = append(r, capturedSentCoinsFromModule{recipientAddr: recipientAddr, coins: amt})
		return nil
	}, &r
}

func CaptureSentCoinsFromAccountFn() (func(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error, *[]capturedSentCoinsFromAddress) {
	var r []capturedSentCoinsFromAddress
	return func(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		r = append(r, capturedSentCoinsFromAddress{recipientModule: recipientModule, coins: amt})
		return nil
	}, &r
}

func CaptureDelegatedCoinsFn() (func(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error, *[]capturedSentCoinsFromAddress) {
	var r []capturedSentCoinsFromAddress
	return func(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		r = append(r, capturedSentCoinsFromAddress{recipientModule: recipientModule, coins: amt})
		return nil
	}, &r
}

func CaptureUndelegatedCoinsFn() (func(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error, *[]capturedSentCoinsFromModule) {
	var r []capturedSentCoinsFromModule
	return func(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
		r = append(r, capturedSentCoinsFromModule{recipientAddr: recipientAddr, coins: amt})
		return nil
	}, &r
}

type ConsensusParamsStoreMock struct {
	GetConsensusParamsFn   func(ctx sdk.Context) *abci.ConsensusParams
	StoreConsensusParamsFn func(ctx sdk.Context, cp *abci.ConsensusParams)
}

func NoopConsensusParamsStoreMock() ConsensusParamsStoreMock {
	return ConsensusParamsStoreMock{
		GetConsensusParamsFn: func(ctx sdk.Context) *abci.ConsensusParams {
			return types.ConsensusParamsFixture()
		},
		StoreConsensusParamsFn: func(ctx sdk.Context, cp *abci.ConsensusParams) {},
	}
}

func (m ConsensusParamsStoreMock) GetConsensusParams(ctx sdk.Context) *abci.ConsensusParams {
	if m.GetConsensusParamsFn == nil {
		panic("not expected to be called")
	}
	return m.GetConsensusParamsFn(ctx)
}

func (m ConsensusParamsStoreMock) StoreConsensusParams(ctx sdk.Context, cp *abci.ConsensusParams) {
	if m.StoreConsensusParamsFn == nil {
		panic("not expected to be called")
	}
	m.StoreConsensusParamsFn(ctx, cp)
}
