package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/starius/api2"
	"gitlab.com/scpcorp/ScPrime/types"
	"gitlab.com/scpcorp/spf-transporter"
	"gitlab.com/scpcorp/spf-transporter/common"
	"gitlab.com/scpcorp/spf-transporter/scprime"
	"gitlab.com/scpcorp/spf-transporter/solana"
	"gitlab.com/scpcorp/spf-transporter/spfemission"
	"gitlab.com/scpcorp/spf-transporter/transporterdb"
)

var (
	QueueSizeGap = types.NewCurrency64(100000)
)

type Config struct {
	ApiAddr       string `short:"a" env:"API_ADDR" default:":9580" description:"host:port that the API server listens on"`
	Dir           string `short:"d" env:"STATE_DIR" default:"." description:"directory where service state is stored"`
	ConsensusPath string `long:"consensus-path" env:"CONSENSUS_PATH" default:"./consensus" description:"path to consensus"`
	DBCfgPath     string `long:"transporter-db-cfg" env:"DB_CFG_PATH" description:"Path to auth DB config"`

	SolanaTxDecayTime        time.Duration `long:"solana-tx-decay-time" env:"SOLANA_TX_DECAY_TIME" default:"3m"`
	QueueCheckInterval       time.Duration `long:"queue-check-interval" env:"QUEUE_CHECK_INTERVAL" default:"30m" `
	UnconfirmedCheckInterval time.Duration `long:"unconfirmed-check-interval" env:"UNCONFIRMED_CHECK_INTERVAL" default:"10m"`
	ScpTxConfirmationTime    time.Duration `long:"scp-tx-confirmation-time" env:"SCP_TX_CONFIRMATION_TIME" default:"1h"`

	TransportMin   string                   `long:"transport-min" env:"TRANSPORT_MIN" default:"100SPF"`
	TransportMax   string                   `long:"transport-limit" env:"TRANSPORT_MAX" default:"20000SPF"`
	QueueSizeLimit string                   `long:"queue-size-limit" env:"QUEUE_SIZE_LIMIT" default:"2500000SPF"`
	QueueLockLimit string                   `long:"queue-lock-limit" env:"QUEUE_LOCL_LIMIT" default:"1000000SPF"` // Is only used if QueueMode is ReleaseAllAtOnce.
	QueueLockGap   time.Duration            `long:"queue-lock-gap" env:"QUEUE_LOCL_GAP" default:"40m"`
	QueueMode      common.QueueHandlingMode `long:"queue-mode" env:"QUEUE_MODE" default:"0"`

	HeliusApiKey     string `long:"helius-api-key" env:"HELIUS_API_KEY"`
	SolanaKeygenFile string `long:"solana-keygen-file" env:"SOLANA_KEYGEN_FILE"`
	SolanaDevnet     bool   `long:"solana-use-devnet" env:"SOLANA_USE_DEVNET"`
}

func TransporterSettingsFromConfig(c Config) (*transporter.Settings, error) {
	transportMin, err := types.NewCurrencyStr(c.TransportMin)
	if err != nil {
		return nil, err
	}
	transportMax, err := types.NewCurrencyStr(c.TransportMax)
	if err != nil {
		return nil, err
	}
	queueSizeLimit, err := types.NewCurrencyStr(c.QueueSizeLimit)
	if err != nil {
		return nil, err
	}
	queueLockLimit, err := types.NewCurrencyStr(c.QueueLockLimit)
	if err != nil {
		return nil, err
	}
	return &transporter.Settings{
		SolanaTxDecayTime:        c.SolanaTxDecayTime,
		QueueCheckInterval:       c.QueueCheckInterval,
		UnconfirmedCheckInterval: c.UnconfirmedCheckInterval,
		ScpTxConfirmationTime:    c.ScpTxConfirmationTime,
		TransportMin:             transportMin,
		TransportMax:             transportMax,
		QueueSizeLimit:           queueSizeLimit,
		QueueLockLimit:           queueLockLimit,
		QueueLockGap:             c.QueueLockGap,
		QueueMode:                c.QueueMode,
	}, nil
}

type Transporter struct {
	server *http.Server

	transporterCloser io.Closer
}

func New() *Transporter {
	return &Transporter{}
}

func (t *Transporter) Start(c Config) error {
	transporterSettings, err := TransporterSettingsFromConfig(c)
	if err != nil {
		return fmt.Errorf("failed to parse transporter settings: %w", err)
	}
	scp, err := scprime.New(filepath.Join(c.Dir, "scprime"))
	if err != nil {
		return fmt.Errorf("failed to initialize scprime: %w", err)
	}
	dbSettings := &transporterdb.Settings{
		PreliminaryQueueSizeLimit: transporterSettings.QueueSizeLimit.Sub(QueueSizeGap),
		QueueSizeLimit:            transporterSettings.QueueSizeLimit,
	}
	pg := transporterdb.OpenPostgresWithRetries(c.DBCfgPath)
	tdb, err := transporterdb.NewDB(pg, dbSettings)
	if err != nil {
		return fmt.Errorf("failed to initialize transporterDB: %w", err)
	}
	var solanaMinter transporter.Solana
	if c.SolanaDevnet {
		solanaMinter, err = solana.NewMinter(context.Background(), solana.NewDevNetConfig(c.SolanaKeygenFile, c.HeliusApiKey))
	} else {
		solanaMinter, err = solana.NewMinter(context.Background(), solana.NewMainNetConfig(c.SolanaKeygenFile, c.HeliusApiKey))
	}
	if err != nil {
		return fmt.Errorf("failed to create solana minter: %w", err)
	}
	srv, err := transporter.New(transporterSettings, spfemission.New(), scp, solanaMinter, tdb)
	if err != nil {
		return fmt.Errorf("could not initialize server: %w", err)
	}

	routes := transporter.GetRoutes(srv)
	mux := http.NewServeMux()
	api2.BindRoutes(mux, routes)

	log.Printf("Listening on %v...", c.ApiAddr)
	t.server = &http.Server{Addr: c.ApiAddr, Handler: mux}
	t.transporterCloser = srv

	go func() {
		if err := t.server.ListenAndServe(); err != nil {
			log.Printf("server.ListenAndServe failed: %v.", err)
		}
	}()

	return nil
}

func (t *Transporter) Close() {
	if t.server != nil {
		if err := t.server.Close(); err != nil {
			log.Printf("server.Close failed: %v.", err)
		}
	}
	if t.transporterCloser != nil {
		if err := t.transporterCloser.Close(); err != nil {
			log.Printf("transporter.Close failed: %v", err)
		}
	}
}
