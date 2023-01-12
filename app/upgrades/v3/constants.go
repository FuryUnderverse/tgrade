package v3

import (
	"github.com/oldfurya/furya/app/upgrades"
)

// UpgradeName defines the on-chain upgrade name for the Petri v3 upgrade.
const UpgradeName = "v3"

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateUpgradeHandler,
}
