package v2

import (
	"github.com/oldfurya/furya/app/upgrades"
)

// UpgradeName defines the on-chain upgrade name for the Petri v2 upgrade.
const UpgradeName = "v2"

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateUpgradeHandler,
}
