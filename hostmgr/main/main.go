package main

import (
	"fmt"
	"net/url"
	"os"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"

	"code.uber.internal/infra/peloton/common"
	"code.uber.internal/infra/peloton/common/config"
	"code.uber.internal/infra/peloton/common/logging"
	"code.uber.internal/infra/peloton/common/metrics"
	"code.uber.internal/infra/peloton/hostmgr"
	"code.uber.internal/infra/peloton/hostmgr/mesos"
	"code.uber.internal/infra/peloton/hostmgr/offer"
	"code.uber.internal/infra/peloton/hostmgr/reconcile"
	"code.uber.internal/infra/peloton/leader"
	"code.uber.internal/infra/peloton/master/task"
	"code.uber.internal/infra/peloton/storage/mysql"
	"code.uber.internal/infra/peloton/yarpc/encoding/mpb"
	"code.uber.internal/infra/peloton/yarpc/peer"
	"code.uber.internal/infra/peloton/yarpc/transport/mhttp"

	"go.uber.org/yarpc"
	"go.uber.org/yarpc/transport"
	"go.uber.org/yarpc/transport/http"

	log "github.com/Sirupsen/logrus"
)

var (
	version string
	app     = kingpin.New("peloton-hostmgr", "Peloton Host Manager")

	debug = app.Flag(
		"debug", "enable debug mode (print full json responses)").
		Short('d').
		Default("false").
		Envar("ENABLE_DEBUG_LOGGING").
		Bool()

	configFiles = app.Flag(
		"config",
		"YAML config files (can be provided multiple times to merge configs)").
		Short('c').
		Required().
		ExistingFiles()

	dbHost = app.Flag(
		"db-host",
		"Database host (db.host override) (set $DB_HOST to override)").
		Envar("DB_HOST").
		String()

	electionZkServers = app.Flag(
		"election-zk-server",
		"Election Zookeeper servers. Specify multiple times for multiple servers "+
			"(election.zk_servers override) (set $ELECTION_ZK_SERVERS to override)").
		Envar("ELECTION_ZK_SERVERS").
		Strings()

	hostmgrPort = app.Flag(
		"port", "Host manager port (hostmgr.port override) (set $PORT to override)").
		Envar("PORT").
		Int()

	zkPath = app.Flag(
		"zk-path",
		"Zookeeper path (mesos.zk_host override) (set $MESOS_ZK_PATH to override)").
		Envar("MESOS_ZK_PATH").
		String()
)

func main() {
	app.Version(version)
	app.HelpFlag.Short('h')
	kingpin.MustParse(app.Parse(os.Args[1:]))

	log.SetFormatter(&log.JSONFormatter{})

	initialLevel := log.InfoLevel
	if *debug {
		initialLevel = log.DebugLevel
	}
	log.SetLevel(initialLevel)

	log.WithField("files", *configFiles).Info("Loading host manager config")
	var cfg Config
	if err := config.Parse(&cfg, *configFiles...); err != nil {
		log.WithField("error", err).Fatal("Cannot parse yaml config")
	}

	// now, override any CLI flags in the loaded config.Config
	if *hostmgrPort != 0 {
		cfg.HostManager.Port = *hostmgrPort
	}

	if *zkPath != "" {
		cfg.Mesos.ZkPath = *zkPath
	}

	if *dbHost != "" {
		cfg.Storage.MySQL.Host = *dbHost
	}

	if len(*electionZkServers) > 0 {
		cfg.Election.ZKServers = *electionZkServers
	}

	log.WithField("config", cfg).Info("Loaded host manager config")

	rootScope, scopeCloser, mux := metrics.InitMetricScope(
		&cfg.Metrics,
		common.PelotonHostManager,
		metrics.TallyFlushInterval,
	)
	defer scopeCloser.Close()

	mux.HandleFunc(logging.LevelOverwrite, logging.LevelOverwriteHandler(initialLevel))

	// NOTE: we "mount" the YARPC endpoints under /yarpc, so we can
	// mux in other HTTP handlers
	inbounds := []transport.Inbound{
		http.NewInbound(
			fmt.Sprintf(":%d", cfg.HostManager.Port),
			http.Mux(common.PelotonEndpointPath, mux),
		),
	}

	// TODO(zhitao): Confirm this code is working when mesos leader changes.
	mesosMasterDetector, err := mesos.NewZKDetector(cfg.Mesos.ZkPath)
	if err != nil {
		log.Fatalf("Failed to initialize mesos master detector: %v", err)
	}

	mesosMasterLocation, err := mesosMasterDetector.GetMasterLocation()
	if err != nil {
		log.Fatalf("Failed to get mesos leading master location, err=%v", err)
	}
	log.Infof("Detected Mesos leading master location: %s", mesosMasterLocation)

	rootScope.Counter("boot").Inc(1)

	// Connect to mysql DB
	if err := cfg.Storage.MySQL.Connect(); err != nil {
		log.Fatalf("Could not connect to database: %+v", err)
	}

	// TODO(wu): Remove this once `make pcluster` is capable to auto
	// migrate the database.  Migrate DB if necessary
	if errs := cfg.Storage.MySQL.AutoMigrate(); errs != nil {
		log.Fatalf("Could not migrate database: %+v", errs)
	}

	store := mysql.NewStore(cfg.Storage.MySQL, rootScope.SubScope("storage"))
	// Tune db connections to make sure that task updates won't leak
	// too many conns
	// TODO: make it common db config for all components to share
	store.DB.SetMaxOpenConns(cfg.HostManager.DbWriteConcurrency)
	store.DB.SetMaxIdleConns(cfg.HostManager.DbWriteConcurrency)
	store.DB.SetConnMaxLifetime(cfg.Storage.MySQL.ConnLifeTime)

	// Initialize YARPC dispatcher with necessary inbounds and outbounds
	driver := mesos.InitSchedulerDriver(&cfg.Mesos, store)

	// Active host manager needs a Mesos inbound
	var mInbound = mhttp.NewInbound(driver)
	inbounds = append(inbounds, mInbound)

	// TODO: update mesos url when leading mesos master changes
	mesosURL := fmt.Sprintf("http://%s%s", mesosMasterLocation, driver.Endpoint())
	mOutbound := mhttp.NewOutbound(mesosURL)

	// all leader discovery metrics share a scope (and will be tagged
	// with role={role})
	discoveryScope := rootScope.SubScope("discovery")
	// setup the discovery service to detect resmgr leaders and
	// configure the YARPC Peer dynamically
	resmgrPeerChooser, err := peer.NewSmartChooser(
		cfg.Election,
		discoveryScope,
		common.ResourceManagerRole,
	)
	if err != nil {
		log.WithFields(log.Fields{"error": err, "role": common.ResourceManagerRole}).
			Fatal("Could not create smart peer chooser")
	}
	if err := resmgrPeerChooser.Start(); err != nil {
		log.WithFields(log.Fields{"error": err, "role": common.ResourceManagerRole}).
			Fatal("Could not start smart peer chooser")
	}
	defer resmgrPeerChooser.Stop()
	resmgrOutbound := http.NewChooserOutbound(
		resmgrPeerChooser,
		&url.URL{
			Scheme: "http",
			Path:   common.PelotonEndpointPath,
		})

	outbounds := yarpc.Outbounds{
		common.MesosMaster: mOutbound,
		common.PelotonResourceManager: transport.Outbounds{
			Unary: resmgrOutbound,
		},
	}

	dispatcher := yarpc.NewDispatcher(yarpc.Config{
		Name:      common.PelotonHostManager,
		Inbounds:  inbounds,
		Outbounds: outbounds,
	})

	// Init the managers driven by the mesos callbacks.
	// They are driven by the leader who will subscribe to
	// mesos callbacks
	mesosClient := mpb.New(
		dispatcher.ClientConfig(common.MesosMaster),
		cfg.Mesos.Encoding,
	)
	mesos.InitManager(dispatcher, &cfg.Mesos, store)

	log.WithFields(log.Fields{
		"port":     cfg.HostManager.Port,
		"url_path": common.PelotonEndpointPath,
	}).Info("HostService initialized")

	offer.InitEventHandler(
		dispatcher,
		rootScope,
		time.Duration(cfg.HostManager.OfferHoldTimeSec)*time.Second,
		time.Duration(cfg.HostManager.OfferPruningPeriodSec)*time.Second,
		mesosClient,
	)

	// Init service handler.
	hostmgr.InitServiceHandler(
		dispatcher,
		rootScope,
		mesosClient,
		driver,
	)

	// Initializing TaskStateManager will start to record task status
	// update back to storage.  TODO(zhitao): This is
	// temporary. Eventually we should create proper API protocol for
	// `WaitTaskStatusUpdate` and allow RM/JM to retrieve this
	// separately.
	task.InitTaskStateManager(
		dispatcher,
		cfg.HostManager.TaskUpdateBufferSize,
		cfg.HostManager.TaskUpdateAckConcurrency,
		common.PelotonResourceManager,
		rootScope,
	)

	reconcile.InitTaskReconciler(
		mesosClient,
		rootScope,
		driver,
		time.Duration(cfg.HostManager.InitialReconcileDelaySec)*time.Second,
		time.Duration(cfg.HostManager.ImplicitReconcileIntervalSec)*time.Second,
	)

	server := hostmgr.NewServer(
		cfg.HostManager.Port,
		mesosMasterDetector,
		mInbound,
		mOutbound,
	)

	candidate, err := leader.NewCandidate(
		cfg.Election,
		rootScope.SubScope("election"),
		common.HostManagerRole,
		server,
	)
	if err != nil {
		log.Fatalf("Unable to create leader candidate: %v", err)
	}
	err = candidate.Start()
	if err != nil {
		log.Fatalf("Unable to start leader candidate: %v", err)
	}
	defer candidate.Stop()

	// Start dispatch loop
	if err := dispatcher.Start(); err != nil {
		log.Fatalf("Could not start rpc server: %v", err)
	}

	select {}
}
