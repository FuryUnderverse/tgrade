package cli

import (
	"fmt"
	"strings"

	wasmcli "github.com/CosmWasm/wasmd/x/wasm/client/cli"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/oldfurya/furya/x/twasm/types"
)

func GetQueryCmd() *cobra.Command {
	queryCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the twasm module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	queryCmd.AddCommand(
		GetCmdShowPrivilegedContracts(),
		GetCmdListPrivilegedContracts(),
	)
	// add all wasmd queries
	queryCmd.AddCommand(wasmcli.GetQueryCmd().Commands()...)
	return queryCmd
}

func GetCmdShowPrivilegedContracts() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list-privileged-by-type <privilege_type>",
		Short:   "List all contract addresses for given privilege type",
		Long:    fmt.Sprintf("List all contracts for privilege type [%s]", strings.Join(types.AllPrivilegeTypeNames(), ", ")),
		Aliases: []string{"privilege-contracts-by-type", "with-privilege", "lpct"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			cbt := types.PrivilegeTypeFrom(args[0])
			if cbt == nil {
				return fmt.Errorf("unknown privilege type: %q", args[0])
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.ContractsByPrivilegeType(
				cmd.Context(),
				&types.QueryContractsByPrivilegeTypeRequest{
					PrivilegeType: cbt.String(),
				},
			)
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdListPrivilegedContracts lists all privileged contracts
func GetCmdListPrivilegedContracts() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list-privileged",
		Short:   "List all privileged contract addresses",
		Long:    "List all contract addresses with privileged permission set",
		Aliases: []string{"privileged-contracts", "privileged", "lpc"},
		Args:    cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.PrivilegedContracts(
				cmd.Context(),
				&types.QueryPrivilegedContractsRequest{},
			)
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
