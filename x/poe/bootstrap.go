package poe

import (
	_ "embed"
	"encoding/json"
	"fmt"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	twasmtypes "github.com/oldfurya/furya/x/twasm/types"

	"github.com/oldfurya/furya/x/poe/contract"
	"github.com/oldfurya/furya/x/poe/keeper"
	"github.com/oldfurya/furya/x/poe/types"
)

var (
	//go:embed contract/pt4_engagement.wasm
	pt4Engagement []byte
	//go:embed contract/pt4_stake.wasm
	pt4Stake []byte
	//go:embed contract/pt4_mixer.wasm
	pt4Mixer []byte
	//go:embed contract/furya_valset.wasm
	tgValset []byte
	//go:embed contract/furya_trusted_circle.wasm
	tgTrustedCircles []byte
	//go:embed contract/furya_oc_proposals.wasm
	tgOCGovProposalsCircles []byte
	//go:embed contract/furya_community_pool.wasm
	tgCommunityPool []byte
	//go:embed contract/furya_validator_voting.wasm
	tgValidatorVoting []byte
	//go:embed contract/furya_ap_voting.wasm
	tgArbiterPool []byte
	//go:embed contract/version.txt
	contractVersion string
)

// ClearEmbeddedContracts release memory
func ClearEmbeddedContracts() {
	pt4Engagement = nil
	pt4Stake = nil
	pt4Mixer = nil
	tgValset = nil
	tgTrustedCircles = nil
	tgOCGovProposalsCircles = nil
	tgCommunityPool = nil
	tgValidatorVoting = nil
	tgArbiterPool = nil
}

type poeKeeper interface {
	keeper.ContractSource
	SetPoEContractAddress(ctx sdk.Context, ctype types.PoEContractType, contractAddr sdk.AccAddress)
	ValsetContract(ctx sdk.Context) keeper.ValsetContract
	EngagementContract(ctx sdk.Context) keeper.EngagementContract
}

// BootstrapPoEContracts stores and instantiates all PoE contracts:
// See https://github.com/oldfurya/furya-contracts/blob/main/docs/Architecture.md#multi-level-governance for an overview
func BootstrapPoEContracts(ctx sdk.Context, k wasmtypes.ContractOpsKeeper, tk twasmKeeper, poeKeeper poeKeeper, gs types.SeedContracts) error {
	bootstrapAccountAddr, err := sdk.AccAddressFromBech32(gs.BootstrapAccountAddress)
	if err != nil {
		return sdkerrors.Wrap(err, "bootstrap account")
	}

	// setup engagement contract
	//
	pt4EngagementInitMsg := newEngagementInitMsg(gs, bootstrapAccountAddr)
	engagementCodeID, _, err := k.Create(ctx, bootstrapAccountAddr, pt4Engagement, &wasmtypes.AllowEverybody)
	if err != nil {
		return sdkerrors.Wrap(err, "store pt4 engagement contract")
	}
	engagementContractAddr, _, err := k.Instantiate(
		ctx,
		engagementCodeID,
		bootstrapAccountAddr,
		bootstrapAccountAddr,
		mustMarshalJSON(pt4EngagementInitMsg),
		"engagement",
		nil,
	)
	if err != nil {
		return sdkerrors.Wrap(err, "instantiate pt4 engagement")
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeEngagement, engagementContractAddr)
	if err := k.PinCode(ctx, engagementCodeID); err != nil {
		return sdkerrors.Wrap(err, "pin pt4 engagement contract")
	}
	logger := keeper.ModuleLogger(ctx)
	logger.Info("engagement group contract", "address", engagementContractAddr, "code_id", engagementCodeID)

	// setup trusted circle for oversight community
	//
	trustedCircleCodeID, _, err := k.Create(ctx, bootstrapAccountAddr, tgTrustedCircles, &wasmtypes.AllowEverybody)
	if err != nil {
		return sdkerrors.Wrap(err, "store tg trusted circle contract")
	}
	ocInitMsg := newOCInitMsg(gs)
	firstOCMember, err := sdk.AccAddressFromBech32(gs.OversightCommunityMembers[0])
	if err != nil {
		return sdkerrors.Wrap(err, "first member")
	}

	ocContractAddr, _, err := k.Instantiate(
		ctx,
		trustedCircleCodeID,
		firstOCMember,
		bootstrapAccountAddr,
		mustMarshalJSON(ocInitMsg),
		"oversight_committee",
		sdk.NewCoins(gs.OversightCommitteeContractConfig.EscrowAmount),
	)
	if err != nil {
		return sdkerrors.Wrap(err, "instantiate tg trusted circle contract")
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeOversightCommunity, ocContractAddr)
	if err := k.PinCode(ctx, trustedCircleCodeID); err != nil {
		return sdkerrors.Wrap(err, "pin tg trusted circle contract")
	}

	if len(gs.OversightCommunityMembers) > 1 {
		err = addToTrustedCircle(ctx, ocContractAddr, tk, gs.OversightCommunityMembers[1:], firstOCMember, gs.OversightCommitteeContractConfig.EscrowAmount)
		if err != nil {
			return err
		}
	}

	logger.Info("oversight community contract", "address", ocContractAddr, "code_id", trustedCircleCodeID)

	// setup stake contract
	//
	stakeCodeID, _, err := k.Create(ctx, bootstrapAccountAddr, pt4Stake, &wasmtypes.AllowEverybody)
	if err != nil {
		return sdkerrors.Wrap(err, "store pt4 stake contract")
	}
	pt4StakeInitMsg := newStakeInitMsg(gs, bootstrapAccountAddr)
	stakeContractAddr, _, err := k.Instantiate(
		ctx,
		stakeCodeID,
		bootstrapAccountAddr,
		bootstrapAccountAddr,
		mustMarshalJSON(pt4StakeInitMsg),
		"stakers",
		nil,
	)
	if err != nil {
		return sdkerrors.Wrap(err, "instantiate pt4 stake")
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeStaking, stakeContractAddr)
	if err := tk.SetPrivileged(ctx, stakeContractAddr); err != nil {
		return sdkerrors.Wrap(err, "grant privileges to stake contract")
	}
	logger.Info("stake contract", "address", stakeContractAddr, "code_id", stakeCodeID)

	poeFunction := contract.Sigmoid{
		MaxPoints: gs.MixerContractConfig.Sigmoid.MaxPoints,
		P:         gs.MixerContractConfig.Sigmoid.P,
		S:         gs.MixerContractConfig.Sigmoid.S,
	}
	pt4MixerInitMsg := contract.TG4MixerInitMsg{
		LeftGroup:        engagementContractAddr.String(),
		RightGroup:       stakeContractAddr.String(),
		PreAuthsSlashing: 1,
		FunctionType: contract.MixerFunction{
			Sigmoid: &poeFunction,
		},
	}
	mixerCodeID, _, err := k.Create(ctx, bootstrapAccountAddr, pt4Mixer, &wasmtypes.AllowEverybody)
	if err != nil {
		return sdkerrors.Wrap(err, "store pt4 mixer contract")
	}
	mixerContractAddr, _, err := k.Instantiate(
		ctx,
		mixerCodeID,
		bootstrapAccountAddr,
		bootstrapAccountAddr,
		mustMarshalJSON(pt4MixerInitMsg),
		"poe",
		nil,
	)
	if err != nil {
		return sdkerrors.Wrap(err, "instantiate pt4 mixer")
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeMixer, mixerContractAddr)
	if err := k.PinCode(ctx, mixerCodeID); err != nil {
		return sdkerrors.Wrap(err, "pin pt4 mixer contract")
	}
	logger.Info("mixer contract", "address", mixerContractAddr, "code_id", mixerCodeID)

	// setup community pool
	//
	communityPoolCodeID, _, err := k.Create(ctx, bootstrapAccountAddr, tgCommunityPool, &wasmtypes.AllowEverybody)
	if err != nil {
		return sdkerrors.Wrap(err, "store community pool contract")
	}
	communityPoolInitMsg := contract.CommunityPoolInitMsg{
		VotingRules:  toContractVotingRules(gs.CommunityPoolContractConfig.VotingRules),
		GroupAddress: engagementContractAddr.String(),
	}
	communityPoolContractAddr, _, err := k.Instantiate(
		ctx,
		communityPoolCodeID,
		bootstrapAccountAddr,
		bootstrapAccountAddr,
		mustMarshalJSON(communityPoolInitMsg),
		"stakers",
		nil,
	)
	if err != nil {
		return sdkerrors.Wrap(err, "instantiate community pool")
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeCommunityPool, communityPoolContractAddr)
	if err := k.PinCode(ctx, communityPoolCodeID); err != nil {
		return sdkerrors.Wrap(err, "pin community pool contract")
	}
	logger.Info("community pool contract", "address", communityPoolContractAddr, "code_id", communityPoolCodeID)

	// setup valset contract
	//
	valSetCodeID, _, err := k.Create(ctx, bootstrapAccountAddr, tgValset, &wasmtypes.AllowEverybody)
	if err != nil {
		return sdkerrors.Wrap(err, "store valset contract")
	}

	valsetInitMsg := newValsetInitMsg(gs, bootstrapAccountAddr, mixerContractAddr, engagementContractAddr, communityPoolContractAddr, engagementCodeID)
	valsetJSON := mustMarshalJSON(valsetInitMsg)
	valsetContractAddr, _, err := k.Instantiate(
		ctx,
		valSetCodeID,
		bootstrapAccountAddr,
		bootstrapAccountAddr,
		valsetJSON,
		"valset",
		nil,
	)
	if err != nil {
		return sdkerrors.Wrapf(err, "instantiate valset with: %s", string(valsetJSON))
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeValset, valsetContractAddr)

	// setup distribution contract address
	//
	valsetCfg, err := poeKeeper.ValsetContract(ctx).QueryConfig(ctx)
	if err != nil {
		return sdkerrors.Wrap(err, "query valset config")
	}

	distrAddr, err := sdk.AccAddressFromBech32(valsetCfg.ValidatorGroup)
	if err != nil {
		return sdkerrors.Wrap(err, "distribution contract address")
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeDistribution, distrAddr)

	if err := tk.SetPrivileged(ctx, valsetContractAddr); err != nil {
		return sdkerrors.Wrap(err, "grant privileges to valset contract")
	}
	logger.Info("valset contract", "address", valsetContractAddr, "code_id", valSetCodeID)

	// setup oversight community gov proposals contract
	//
	ocGovCodeID, _, err := k.Create(ctx, bootstrapAccountAddr, tgOCGovProposalsCircles, &wasmtypes.AllowEverybody)
	if err != nil {
		return sdkerrors.Wrap(err, "store tg oc gov proposals contract: ")
	}
	ocGovInitMsg := newOCGovProposalsInitMsg(gs, ocContractAddr, engagementContractAddr, valsetContractAddr)
	ocGovProposalsContractAddr, _, err := k.Instantiate(
		ctx,
		ocGovCodeID,
		bootstrapAccountAddr,
		bootstrapAccountAddr,
		mustMarshalJSON(ocGovInitMsg),
		"oversight_committee gov proposals",
		nil,
	)
	if err != nil {
		return sdkerrors.Wrap(err, "instantiate tg oc gov proposals contract")
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeOversightCommunityGovProposals, ocGovProposalsContractAddr)
	if err := k.PinCode(ctx, ocGovCodeID); err != nil {
		return sdkerrors.Wrap(err, "pin tg oc gov proposals contract")
	}
	logger.Info("oversight community gov proposal contract", "address", ocGovProposalsContractAddr, "code_id", ocGovCodeID)

	err = poeKeeper.EngagementContract(ctx).UpdateAdmin(ctx, ocGovProposalsContractAddr, bootstrapAccountAddr)
	if err != nil {
		return sdkerrors.Wrap(err, "set new engagement contract admin")
	}

	err = poeKeeper.ValsetContract(ctx).UpdateAdmin(ctx, ocGovProposalsContractAddr, bootstrapAccountAddr)
	if err != nil {
		return sdkerrors.Wrap(err, "set new valset contract admin")
	}

	// setup validator voting contract
	//
	validatorVotingCodeID, _, err := k.Create(ctx, bootstrapAccountAddr, tgValidatorVoting, &wasmtypes.AllowEverybody)
	if err != nil {
		return sdkerrors.Wrap(err, "store validator voting contract")
	}
	validatorVotingInitMsg := contract.ValidatorVotingInitMsg{
		VotingRules:  toContractVotingRules(gs.ValidatorVotingContractConfig.VotingRules),
		GroupAddress: distrAddr.String(),
	}
	validatorVotingContractAddr, _, err := k.Instantiate(
		ctx,
		validatorVotingCodeID,
		bootstrapAccountAddr,
		bootstrapAccountAddr,
		mustMarshalJSON(validatorVotingInitMsg),
		"stakers",
		nil,
	)
	if err != nil {
		return sdkerrors.Wrap(err, "instantiate validator voting")
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeValidatorVoting, validatorVotingContractAddr)

	if err := tk.SetPrivileged(ctx, validatorVotingContractAddr); err != nil {
		return sdkerrors.Wrap(err, "grant privileges to validator voting contract")
	}
	logger.Info("validator voting contract", "address", validatorVotingContractAddr, "code_id", validatorVotingCodeID)

	// setup trusted circle for ap
	apTrustedCircleInitMsg := newAPTrustedCircleInitMsg(gs)
	firstAPMember, err := sdk.AccAddressFromBech32(gs.ArbiterPoolMembers[0])
	if err != nil {
		return sdkerrors.Wrap(err, "first ap member")
	}

	apContractAddr, _, err := k.Instantiate(
		ctx,
		trustedCircleCodeID,
		firstAPMember,
		bootstrapAccountAddr,
		mustMarshalJSON(apTrustedCircleInitMsg),
		"arbiter_pool",
		sdk.NewCoins(gs.ArbiterPoolContractConfig.EscrowAmount),
	)
	if err != nil {
		return sdkerrors.Wrap(err, "instantiate tg trusted circle contract")
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeArbiterPool, apContractAddr)
	if len(gs.ArbiterPoolMembers) > 1 {
		err = addToTrustedCircle(ctx, apContractAddr, tk, gs.ArbiterPoolMembers[1:], firstAPMember, gs.ArbiterPoolContractConfig.EscrowAmount)
		if err != nil {
			return err
		}
	}

	// setup arbiter pool
	apCodeID, _, err := k.Create(ctx, bootstrapAccountAddr, tgArbiterPool, &wasmtypes.AllowEverybody)
	if err != nil {
		return sdkerrors.Wrap(err, "store arbiter voting contract: ")
	}
	apVotingInitMsg := newArbiterPoolVotingInitMsg(gs, apContractAddr)
	apVotingContractAddr, _, err := k.Instantiate(
		ctx,
		apCodeID,
		bootstrapAccountAddr,
		bootstrapAccountAddr,
		mustMarshalJSON(apVotingInitMsg),
		"arbiter pool voting",
		nil,
	)
	if err != nil {
		return sdkerrors.Wrap(err, "instantiate tg ap voting contract")
	}
	poeKeeper.SetPoEContractAddress(ctx, types.PoEContractTypeArbiterPoolVoting, apVotingContractAddr)
	if err := k.PinCode(ctx, apCodeID); err != nil {
		return sdkerrors.Wrap(err, "pin tg ap voting contract")
	}
	logger.Info("arbiter pool voting contract", "address", apVotingContractAddr, "code_id", apCodeID)

	if err := setAllPoEContractsInstanceMigrators(ctx, k, poeKeeper, bootstrapAccountAddr, validatorVotingContractAddr); err != nil {
		return sdkerrors.Wrap(err, "set new instance admin")
	}
	keeper.ModuleLogger(ctx).Info("Seeded PoE contracts", "version", contractVersion)
	return nil
}

func addToTrustedCircle(ctx sdk.Context, contractAddr sdk.AccAddress, tk types.TWasmKeeper, members []string, sender sdk.AccAddress, deposit sdk.Coin) error {
	tcAdapter := contract.NewTrustedCircleContractAdapter(contractAddr, tk, nil)
	err := tcAdapter.AddVotingMembersProposal(ctx, members, sender)
	if err != nil {
		return sdkerrors.Wrap(err, "add voting members proposal")
	}
	latest, err := tcAdapter.LatestProposal(ctx)
	if err != nil {
		return sdkerrors.Wrap(err, "query latest proposal")
	}
	err = tcAdapter.ExecuteProposal(ctx, latest.ID, sender)
	if err != nil {
		return sdkerrors.Wrap(err, "execute proposal")
	}
	// deposit escrow
	for _, member := range members {
		addr, err := sdk.AccAddressFromBech32(member)
		if err != nil {
			return sdkerrors.Wrapf(err, "%s member", member)
		}
		err = tcAdapter.DepositEscrow(ctx, deposit, addr)
		if err != nil {
			return sdkerrors.Wrapf(err, "%s deposit escrow", addr)
		}
	}
	return nil
}

// set new migrator for all PoE contracts
func setAllPoEContractsInstanceMigrators(ctx sdk.Context, k wasmtypes.ContractOpsKeeper, poeKeeper keeper.ContractSource, oldAdminAddr, newAdminAddr sdk.AccAddress) error {
	var rspErr error
	types.IteratePoEContractTypes(func(tp types.PoEContractType) bool {
		addr, err := poeKeeper.GetPoEContractAddress(ctx, tp)
		if err != nil {
			rspErr = sdkerrors.Wrapf(err, "failed to find contract address for %s", tp.String())
			return true
		}
		if err := k.UpdateContractAdmin(ctx, addr, oldAdminAddr, newAdminAddr); err != nil {
			rspErr = sdkerrors.Wrapf(err, "%s contract", tp.String())
		}
		return rspErr != nil
	})
	return rspErr
}

// build instantiate message for the trusted circle contract that contains the oversight committee
func newOCInitMsg(gs types.SeedContracts) contract.TrustedCircleInitMsg {
	cfg := gs.OversightCommitteeContractConfig
	return contract.TrustedCircleInitMsg{
		Name:                      cfg.Name,
		Denom:                     cfg.EscrowAmount.Denom,
		EscrowAmount:              cfg.EscrowAmount.Amount,
		VotingPeriod:              cfg.VotingRules.VotingPeriod,
		Quorum:                    *contract.DecimalFromPercentage(cfg.VotingRules.Quorum),
		Threshold:                 *contract.DecimalFromPercentage(cfg.VotingRules.Threshold),
		AllowEndEarly:             cfg.VotingRules.AllowEndEarly,
		InitialMembers:            []string{}, // sender is added to OC by default in the contract
		DenyList:                  cfg.DenyListContractAddress,
		EditTrustedCircleDisabled: true, // product requirement for OC
		RewardDenom:               cfg.EscrowAmount.Denom,
	}
}

// build instantiate message for OC Proposals contract
func newOCGovProposalsInitMsg(gs types.SeedContracts, ocContract, engagementContract, valsetContract sdk.AccAddress) contract.OCProposalsInitMsg {
	cfg := gs.OversightCommitteeContractConfig
	return contract.OCProposalsInitMsg{
		GroupContractAddress:      ocContract.String(),
		ValsetContractAddress:     valsetContract.String(),
		EngagementContractAddress: engagementContract.String(),
		VotingRules:               toContractVotingRules(cfg.VotingRules),
	}
}

// build instantiate message for the trusted circle contract that contains the arbiter pool
func newAPTrustedCircleInitMsg(gs types.SeedContracts) contract.TrustedCircleInitMsg {
	cfg := gs.ArbiterPoolContractConfig
	return contract.TrustedCircleInitMsg{
		Name:                      cfg.Name,
		Denom:                     cfg.EscrowAmount.Denom,
		EscrowAmount:              cfg.EscrowAmount.Amount,
		VotingPeriod:              cfg.VotingRules.VotingPeriod,
		Quorum:                    *contract.DecimalFromPercentage(cfg.VotingRules.Quorum),
		Threshold:                 *contract.DecimalFromPercentage(cfg.VotingRules.Threshold),
		AllowEndEarly:             cfg.VotingRules.AllowEndEarly,
		InitialMembers:            []string{}, // sender is added to AP by default in the contract
		DenyList:                  cfg.DenyListContractAddress,
		EditTrustedCircleDisabled: true,
		RewardDenom:               cfg.EscrowAmount.Denom,
	}
}

// build instantiate message for AP contract
func newArbiterPoolVotingInitMsg(gs types.SeedContracts, apContract sdk.AccAddress) contract.APVotingInitMsg {
	cfg := gs.ArbiterPoolContractConfig
	return contract.APVotingInitMsg{
		GroupContractAddress: apContract.String(),
		VotingRules:          toContractVotingRules(cfg.VotingRules),
		WaitingPeriod:        uint64(cfg.WaitingPeriod.Seconds()),
		DisputeCost:          cfg.DisputeCost,
	}
}

func newEngagementInitMsg(gs types.SeedContracts, bootstrapAccountAddr sdk.AccAddress) contract.TG4EngagementInitMsg {
	pt4EngagementInitMsg := contract.TG4EngagementInitMsg{
		Admin:            bootstrapAccountAddr.String(),
		Members:          make([]contract.TG4Member, len(gs.Engagement)),
		PreAuthsHooks:    1,
		PreAuthsSlashing: 1,
		Denom:            gs.BondDenom,
		Halflife:         uint64(gs.EngagementContractConfig.Halflife.Seconds()),
	}
	for i, v := range gs.Engagement {
		pt4EngagementInitMsg.Members[i] = contract.TG4Member{
			Addr:   v.Address,
			Points: v.Points,
		}
	}
	return pt4EngagementInitMsg
}

func newStakeInitMsg(gs types.SeedContracts, adminAddr sdk.AccAddress) contract.TG4StakeInitMsg {
	claimLimit := uint64(gs.StakeContractConfig.ClaimAutoreturnLimit)
	return contract.TG4StakeInitMsg{
		Admin:            adminAddr.String(),
		Denom:            gs.BondDenom,
		MinBond:          gs.StakeContractConfig.MinBond,
		TokensPerPoint:   gs.StakeContractConfig.TokensPerPoint,
		UnbondingPeriod:  uint64(gs.StakeContractConfig.UnbondingPeriod.Seconds()),
		AutoReturnLimit:  &claimLimit,
		PreAuthsHooks:    1,
		PreAuthsSlashing: 1,
	}
}

func newValsetInitMsg(
	gs types.SeedContracts,
	admin sdk.AccAddress,
	mixerContractAddr sdk.AccAddress,
	engagementAddr sdk.AccAddress,
	communityPoolAddr sdk.AccAddress,
	engagementCodeID uint64,
) contract.ValsetInitMsg {
	config := gs.ValsetContractConfig
	return contract.ValsetInitMsg{
		Admin:               admin.String(),
		Membership:          mixerContractAddr.String(),
		MinPoints:           config.MinPoints,
		MaxValidators:       config.MaxValidators,
		EpochLength:         uint64(config.EpochLength.Seconds()),
		EpochReward:         config.EpochReward,
		InitialKeys:         []contract.Validator{},
		Scaling:             config.Scaling,
		FeePercentage:       contract.DecimalFromPercentage(config.FeePercentage),
		AutoUnjail:          config.AutoUnjail,
		VerifyValidators:    config.VerifyValidators,
		OfflineJailDuration: uint64(config.OfflineJailDuration.Seconds()),
		DistributionContracts: []contract.DistributionContract{
			{Address: engagementAddr.String(), Ratio: *contract.DecimalFromPercentage(config.EngagementRewardRatio)},
			{Address: communityPoolAddr.String(), Ratio: *contract.DecimalFromPercentage(config.CommunityPoolRewardRatio)},
		},
		ValidatorGroupCodeID: engagementCodeID,
	}
}

// VerifyPoEContracts sanity check that verifies all PoE contracts are set up as expected
func VerifyPoEContracts(ctx sdk.Context, tk twasmKeeper, poeKeeper keeper.ContractSource) error {
	valVotingContractAddr, err := poeKeeper.GetPoEContractAddress(ctx, types.PoEContractTypeValidatorVoting)
	if err != nil {
		return sdkerrors.Wrap(err, "validator voting address")
	}
	types.IteratePoEContractTypes(func(tp types.PoEContractType) bool {
		var addr sdk.AccAddress
		addr, err = poeKeeper.GetPoEContractAddress(ctx, tp)
		if err != nil {
			return true
		}
		// migrator set to validator voting contract address
		c := tk.GetContractInfo(ctx, addr)
		if c == nil {
			err = sdkerrors.Wrapf(types.ErrInvalid, "unknown contract: %s", addr)
			return true
		}
		if c.Admin != valVotingContractAddr.String() {
			err = sdkerrors.Wrapf(types.ErrInvalid, "admin address: %s", c.Admin)
			return true
		}
		// all poe contracts pinned
		if !tk.IsPinnedCode(ctx, c.CodeID) {
			keeper.ModuleLogger(ctx).Error("PoE contract is not pinned", "name", tp.String(), "code-id", c.CodeID, "address", addr.String())
			// fail when https://github.com/oldfurya/furya/issues/402 is implemented
			// err = sdkerrors.Wrapf(types.ErrInvalid, "code %d not pinned for poe contract :%s", c.CodeID, tp.String())
			// return true
		}
		return false
	})
	if err != nil { // return any error from within the iteration
		return err
	}
	// verify PoE setup
	addr, err := poeKeeper.GetPoEContractAddress(ctx, types.PoEContractTypeValset)
	if err != nil {
		return sdkerrors.Wrap(err, "valset addr")
	}
	switch ok, err := tk.HasPrivilegedContract(ctx, addr, twasmtypes.PrivilegeTypeValidatorSetUpdate); {
	case err != nil:
		return sdkerrors.Wrap(err, "valset contract")
	case !ok:
		return sdkerrors.Wrap(types.ErrInvalid, "valset contract not registered for validator updates")
	}
	// staking contract must be 	privileged and registered for delegations
	stakeContractAddr, err := poeKeeper.GetPoEContractAddress(ctx, types.PoEContractTypeStaking)
	if err != nil {
		return sdkerrors.Wrap(err, "validator voting address")
	}
	ok, err := tk.HasPrivilegedContract(ctx, stakeContractAddr, twasmtypes.PrivilegeDelegator)
	if err != nil {
		return sdkerrors.Wrap(err, "staking contract")
	}
	if !ok {
		return sdkerrors.Wrap(types.ErrInvalid, "no contract with delegator privileges")
	}
	return nil
}

// mustMarshalJSON with stdlib json
func mustMarshalJSON(s interface{}) []byte {
	jsonBz, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal json: %s", err))
	}
	return jsonBz
}

// map to contract object
func toContractVotingRules(votingRules types.VotingRules) contract.VotingRules {
	return contract.VotingRules{
		VotingPeriod:  votingRules.VotingPeriod,
		Quorum:        *contract.DecimalFromPercentage(votingRules.Quorum),
		Threshold:     *contract.DecimalFromPercentage(votingRules.Threshold),
		AllowEndEarly: votingRules.AllowEndEarly,
	}
}
