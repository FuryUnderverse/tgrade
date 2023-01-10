package app

import (
	"github.com/cosmos/cosmos-sdk/std"

	appparams "github.com/blackfury-1/petri/app/params"
)

// MakeEncodingConfig creates a new EncodingConfig with all modules registered
func MakeEncodingConfig() appparams.EncodingConfig {
	encodingConfig := appparams.MakeEncodingConfig()
	std.RegisterLegacyAminoCodec(encodingConfig.Amino)
	std.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	ModuleBasics.RegisterLegacyAminoCodec(encodingConfig.Amino)
	ModuleBasics.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	return encodingConfig
}
