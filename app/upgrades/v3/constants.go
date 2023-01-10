package v3

import (
	"github.com/blackfury-1/petri/app/upgrades"
)

// UpgradeName defines the on-chain upgrade name for the Petri v3 upgrade.
const UpgradeName = "v3"

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateUpgradeHandler,
}
