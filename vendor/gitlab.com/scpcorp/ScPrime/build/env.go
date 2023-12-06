package build

const (
	// EnvvarAPIPassword is the environment variable that sets a custom API
	// password if the default is not used
	EnvvarAPIPassword = "SCPRIME_API_PASSWORD" //nolint:gosec

	// EnvvarMetaDataDir is the environment variable that tells spd where to put the
	// sia data
	EnvvarMetaDataDir = "SCPRIME_DATA_DIR"

	// EnvvarWalletPassword is the environment variable that can be set to enable
	// auto unlocking the wallet
	EnvvarWalletPassword = "SCPRIME_WALLET_PASSWORD" //nolint:gosec

	// EnvvarDaemonDataDir is the environment variable which tells the daemon
	// where to put the daemon specific data
	EnvvarDaemonDataDir = "DAEMON_DATA_DIR"
)
