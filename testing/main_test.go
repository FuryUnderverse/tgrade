//go:build system_test

package testing

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/tendermint/tendermint/libs/rand"

	"github.com/oldfurya/furya/app"
)

var (
	sut     *SystemUnderTest
	verbose bool
)

func init() {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(app.Bech32PrefixConsAddr, app.Bech32PrefixConsPub)
}

func TestMain(m *testing.M) {
	rebuild := flag.Bool("rebuild", false, "rebuild artifacts")
	waitTime := flag.Duration("wait-time", defaultWaitTime, "time to wait for chain events")
	nodesCount := flag.Int("nodes-count", 4, "number of nodes in the cluster")
	blockTime := flag.Duration("block-time", 1000*time.Millisecond, "block creation time")
	flag.BoolVar(&verbose, "verbose", false, "verbose output")
	flag.Parse()

	// fail fast on most common setup issue
	requireEnoughFileHandlers(*nodesCount + 1) // +1 as tests may start another node

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	workDir = filepath.Join(dir, "../")
	if verbose {
		println("Work dir: ", workDir)
	}
	defaultWaitTime = *waitTime
	sut = NewSystemUnderTest(verbose, *nodesCount, *blockTime)
	if *rebuild {
		sut.BuildNewBinary()
	}
	// setup chain and keyring
	sut.SetupChain()

	// run tests
	exitCode := m.Run()

	// postprocess
	sut.StopChain()
	if verbose || exitCode != 0 {
		sut.PrintBuffer()
		printResultFlag(exitCode == 0)
	}

	os.Exit(exitCode)
}

// requireEnoughFileHandlers uses `ulimit`
func requireEnoughFileHandlers(nodesCount int) error {
	ulimit, err := exec.LookPath("ulimit")
	if err != nil || ulimit == "" { // skip when not available
		return nil
	}

	cmd := exec.Command(ulimit, "-n")
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("unexpected error :%#+v, output: %s", err, string(out)))
	}
	fileDescrCount, err := strconv.Atoi(strings.Trim(string(out), " \t\n"))
	if err != nil {
		panic(fmt.Sprintf("unexpected error :%#+v, output: %s", err, string(out)))
	}
	expFH := nodesCount * 260 // random number that worked on my box
	if fileDescrCount < expFH {
		panic(fmt.Sprintf("Fail fast. Insufficient setup. Run 'ulimit -n %d'", expFH))
	}
	return err
}

const (
	successFlag = `
 ___ _   _  ___ ___ ___  ___ ___ 
/ __| | | |/ __/ __/ _ \/ __/ __|
\__ \ |_| | (_| (_|  __/\__ \__ \
|___/\__,_|\___\___\___||___/___/`
	failureFlag = `
  __      _ _          _ 
 / _|    (_) |        | |
| |_ __ _ _| | ___  __| |
|  _/ _| | | |/ _ \/ _| |
| || (_| | | |  __/ (_| |
|_| \__,_|_|_|\___|\__,_|`
)

func printResultFlag(ok bool) {
	if ok {
		fmt.Println(successFlag)
	} else {
		fmt.Println(failureFlag)
	}
}

func randomBech32Addr() string {
	src := rand.Bytes(address.Len)
	return encodeBech32Addr(src)
}

func encodeBech32Addr(src []byte) string {
	bech32Addr, err := bech32.ConvertAndEncode("furya", src)
	if err != nil {
		panic(err.Error())
	}
	return bech32Addr
}

// ContractBech32Address build a furya bech32 contract address
func ContractBech32Address(codeID, instanceID uint64) string {
	return encodeBech32Addr(wasmkeeper.BuildContractAddressClassic(codeID, instanceID))
}

func AwaitValsetEpochCompleted(t *testing.T) {
	// wait for update manifests in valset (epoch has completed)
	time.Sleep(sutEpochDuration)
	sut.AwaitNextBlock(t)
}

func toJson(t *testing.T, o interface{}) string {
	bz, err := json.Marshal(o)
	require.NoError(t, err)
	return string(bz)
}
