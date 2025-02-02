package poe

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"

	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/oldfurya/furya/x/poe/client/cli"
	"github.com/oldfurya/furya/x/poe/contract"
	"github.com/oldfurya/furya/x/poe/keeper"
	"github.com/oldfurya/furya/x/poe/simulation"
	"github.com/oldfurya/furya/x/poe/types"
	twasmtypes "github.com/oldfurya/furya/x/twasm/types"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic defines the basic application module used by the genutil module.
type AppModuleBasic struct{}

// Name returns the genutil module's name.
func (AppModuleBasic) Name() string {
	return types.ModuleName
}

// RegisterLegacyAminoCodec registers the genutil module's types on the given LegacyAmino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(amino *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(amino)
}

// RegisterInterfaces registers the module's interface types
func (b AppModuleBasic) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
	slashingtypes.RegisterInterfaces(registry)
	stakingtypes.RegisterInterfaces(registry)
}

// DefaultGenesis returns default genesis state as raw bytes for the genutil
// module.
func (b AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// ValidateGenesis performs genesis state validation for the genutil module.
func (b AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, txEncodingConfig client.TxEncodingConfig, bz json.RawMessage) error {
	var data types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return types.ValidateGenesis(data, txEncodingConfig.TxJSONDecoder())
}

// RegisterRESTRoutes registers the REST routes for the genutil module.
func (AppModuleBasic) RegisterRESTRoutes(_ client.Context, _ *mux.Router) {}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the genutil module.
func (b AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, serveMux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), serveMux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
	// support cosmos queries
	if err := slashingtypes.RegisterQueryHandlerClient(context.Background(), serveMux, slashingtypes.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
	if err := stakingtypes.RegisterQueryHandlerClient(context.Background(), serveMux, stakingtypes.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
	if err := distributiontypes.RegisterQueryHandlerClient(context.Background(), serveMux, distributiontypes.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// GetTxCmd returns no root tx command for the genutil module.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return cli.NewTxCmd() }

// GetQueryCmd returns no root query command for the genutil module.
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	return cli.GetQueryCmd()
}

// AppModule implements an application module for the genutil module.
type AppModule struct {
	AppModuleBasic
	deliverTx        DeliverTxfn
	txEncodingConfig client.TxEncodingConfig
	twasmKeeper      twasmKeeper
	contractKeeper   wasmtypes.ContractOpsKeeper
	poeKeeper        *keeper.Keeper
	bankKeeper       simulation.BankKeeper
	accountKeeper    types.AccountKeeper
}

// twasmKeeper subset of keeper to decouple from twasm module
type twasmKeeper interface {
	types.TWasmKeeper
	endBlockKeeper
	SetPrivileged(ctx sdk.Context, contractAddr sdk.AccAddress) error
	HasPrivilegedContract(ctx sdk.Context, contractAddr sdk.AccAddress, privilegeType twasmtypes.PrivilegeType) (bool, error)
	IsPinnedCode(ctx sdk.Context, codeID uint64) bool
	GetContractInfo(ctx sdk.Context, contractAddress sdk.AccAddress) *wasmtypes.ContractInfo
}

// NewAppModule creates a new AppModule object
func NewAppModule(poeKeeper *keeper.Keeper, twasmKeeper twasmKeeper, bankKeeper simulation.BankKeeper, accountKeeper types.AccountKeeper, deliverTx DeliverTxfn, txEncodingConfig client.TxEncodingConfig, contractKeeper wasmtypes.ContractOpsKeeper) AppModule {
	return AppModule{
		AppModuleBasic:   AppModuleBasic{},
		twasmKeeper:      twasmKeeper,
		contractKeeper:   contractKeeper,
		poeKeeper:        poeKeeper,
		deliverTx:        deliverTx,
		txEncodingConfig: txEncodingConfig,
		bankKeeper:       bankKeeper, // used by simulations only
		accountKeeper:    accountKeeper,
	}
}

func (am AppModule) RegisterInvariants(registry sdk.InvariantRegistry) {
}

func (am AppModule) Route() sdk.Route {
	return sdk.NewRoute(types.RouterKey, NewHandler(am.poeKeeper, am.contractKeeper, am.twasmKeeper))
}

func (am AppModule) QuerierRoute() string {
	return types.QuerierRoute
}

func (am AppModule) LegacyQuerierHandler(amino *codec.LegacyAmino) sdk.Querier {
	return nil
}

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQuerier(am.poeKeeper))
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.poeKeeper, am.contractKeeper, am.twasmKeeper))

	// support cosmos query path
	stakingtypes.RegisterQueryServer(cfg.QueryServer(), keeper.NewLegacyStakingGRPCQuerier(am.poeKeeper))
	slashingtypes.RegisterQueryServer(cfg.QueryServer(), keeper.NewLegacySlashingGRPCQuerier(am.poeKeeper))
	distributiontypes.RegisterQueryServer(cfg.QueryServer(), keeper.NewLegacyDistributionGRPCQuerier(am.poeKeeper))
}

func (am AppModule) BeginBlock(ctx sdk.Context, block abci.RequestBeginBlock) {
	BeginBlocker(ctx, am.poeKeeper, block)
}

func (am AppModule) EndBlock(ctx sdk.Context, block abci.RequestEndBlock) []abci.ValidatorUpdate {
	ClearEmbeddedContracts() // release memory
	return EndBlocker(ctx, am.twasmKeeper)
}

// InitGenesis performs genesis initialization for the genutil module. It returns
// no validator updates.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) []abci.ValidatorUpdate {
	var genesisState types.GenesisState
	cdc.MustUnmarshalJSON(data, &genesisState)

	seedMode := genesisState.GetSeedContracts() != nil
	if seedMode {
		if len(genesisState.GetSeedContracts().GenTxs) == 0 {
			panic(sdkerrors.Wrap(wasmtypes.ErrInvalidGenesis, "empty gentx"))
		}
		if err := BootstrapPoEContracts(ctx, am.contractKeeper, am.twasmKeeper, am.poeKeeper, *genesisState.GetSeedContracts()); err != nil {
			panic(fmt.Sprintf("bootstrap PoE contracts: %+v", err))
		}
	}
	if err := keeper.InitGenesis(ctx, am.poeKeeper, am.deliverTx, genesisState, am.txEncodingConfig); err != nil {
		panic(err)
	}

	// verify PoE setup
	if err := VerifyPoEContracts(ctx, am.twasmKeeper, am.poeKeeper); err != nil {
		panic(fmt.Sprintf("verify PoE bootstrap contracts: %+v", err))
	}

	addr, err := am.poeKeeper.GetPoEContractAddress(ctx, types.PoEContractTypeValset)
	if err != nil {
		panic(fmt.Sprintf("valset addr: %s", err))
	}
	if seedMode {
		// query validators from PoE for initial abci set
		switch initialSet, err := contract.CallEndBlockWithValidatorUpdate(ctx, addr, am.twasmKeeper); {
		case err != nil:
			panic(fmt.Sprintf("poe sudo call: %s", err))
		case len(initialSet) == 0:
			panic("initial valset must not be empty")
		default:
			return initialSet
		}
	}
	// in dump import mode
	// query and return the active validator set
	var activeSet []abci.ValidatorUpdate
	err = am.poeKeeper.ValsetContract(ctx).IterateActiveValidators(ctx, func(c contract.ValidatorInfo) bool {
		pub, err := contract.ConvertToTendermintPubKey(c.ValidatorPubkey)
		if err != nil {
			panic(fmt.Sprintf("convert pubkey for %s", c.Operator))
		}
		activeSet = append(activeSet, abci.ValidatorUpdate{
			PubKey: pub,
			Power:  int64(c.Power),
		})
		return false
	}, nil)
	if err != nil {
		panic(fmt.Sprintf("iterate active validators: %s", err))
	}
	if len(activeSet) == 0 { // fal fast
		panic("active valset must not be empty")
	}
	return activeSet
}

// ExportGenesis returns the exported genesis state as raw bytes for the genutil
// module.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs := keeper.ExportGenesis(ctx, am.poeKeeper)
	return cdc.MustMarshalJSON(gs)
}

// ConsensusVersion is a sequence number for state-breaking change of the
// module. It should be incremented on each consensus-breaking change
// introduced by the module. To avoid wrong/empty versions, the initial version
// should be set to 1.
func (am AppModule) ConsensusVersion() uint64 {
	return 1
}

// GenerateGenesisState creates a randomized GenState of the PoE module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomizedGenState(simState)
}

// ProposalContents doesn't return any content functions for governance proposals.
func (AppModule) ProposalContents(simState module.SimulationState) []simtypes.WeightedProposalContent {
	return nil
}

// RandomizedParams creates randomized PoE param changes for the simulator.
func (AppModule) RandomizedParams(r *rand.Rand) []simtypes.ParamChange {
	return nil
}

// RegisterStoreDecoder registers a decoder for PoE module's types
func (am AppModule) RegisterStoreDecoder(sdr sdk.StoreDecoderRegistry) {
}

// WeightedOperations returns the all the PoE module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	return simulation.WeightedOperations(
		simState.AppParams, simState.Cdc, am.bankKeeper, am.accountKeeper, am.poeKeeper,
	)
}
