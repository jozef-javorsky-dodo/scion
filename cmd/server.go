package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ptone/scion-agent/pkg/agent"
	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/apiclient"
	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/harness"
	"github.com/ptone/scion-agent/pkg/hub"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/ptone/scion-agent/pkg/runtimehost"
	"github.com/ptone/scion-agent/pkg/storage"
	"github.com/ptone/scion-agent/pkg/store"
	"github.com/ptone/scion-agent/pkg/store/sqlite"
	"github.com/spf13/cobra"
)

// GlobalGroveName is the special name for the default grove when hub and runtime-host run together
const GlobalGroveName = "global"

var (
	serverConfigPath  string
	hubPort           int
	hubHost           string
	enableHub         bool
	enableRuntimeHost bool
	runtimeHostPort   int
	dbURL             string
	enableDevAuth     bool
	enableDebug       bool
	storageBucket     string
	storageDir        string

	// Template cache settings for Runtime Host
	templateCacheDir string
	templateCacheMax int64
)

// serverCmd represents the server command
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage the Scion server components",
	Long: `Commands for managing the Scion server components.

The server provides:
- Hub API: Central registry for groves, agents, and templates (port 9810)
- Runtime Host API: Agent lifecycle management on compute nodes (port 9800)
- Web Frontend: Browser-based UI (coming soon, port 9820)`,
}

// serverStartCmd represents the server start command
var serverStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Scion server components",
	Long: `Start one or more Scion server components.

Server Components:
- Hub API (--enable-hub): Central coordination for groves, agents, templates
- Runtime Host API (--enable-runtime-host): Agent lifecycle on this compute node

Configuration can be provided via:
- Config file (--config flag or ~/.scion/server.yaml)
- Environment variables (SCION_SERVER_* prefix)
- Command-line flags

Examples:
  # Start Hub API only
  scion server start --enable-hub

  # Start Runtime Host API only
  scion server start --enable-runtime-host

  # Start both Hub and Runtime Host
  scion server start --enable-hub --enable-runtime-host

  # Start Runtime Host with custom port
  scion server start --enable-runtime-host --runtime-host-port 9800`,
	RunE: runServerStart,
}

// portStatus represents the result of checking a port.
type portStatus struct {
	inUse        bool
	isScionServer bool
}

// checkPort checks if a port is already bound and if it's a scion server.
func checkPort(host string, port int) portStatus {
	addr := fmt.Sprintf("%s:%d", host, port)
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		ln.Close()
		return portStatus{inUse: false}
	}

	// Port is in use - check if it's a scion server by hitting the health endpoint
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/healthz", addr))
	if err != nil {
		return portStatus{inUse: true, isScionServer: false}
	}
	defer resp.Body.Close()

	// Check if the response looks like a scion health response
	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Uptime  string `json:"uptime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return portStatus{inUse: true, isScionServer: false}
	}

	// If we got valid health response fields, it's a scion server
	if health.Status != "" && health.Uptime != "" {
		return portStatus{inUse: true, isScionServer: true}
	}

	return portStatus{inUse: true, isScionServer: false}
}

func runServerStart(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.LoadGlobalConfig(serverConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Override with command-line flags if specified
	if cmd.Flags().Changed("port") {
		cfg.Hub.Port = hubPort
	}
	if cmd.Flags().Changed("host") {
		cfg.Hub.Host = hubHost
	}
	if cmd.Flags().Changed("db") {
		cfg.Database.URL = dbURL
	}
	if cmd.Flags().Changed("enable-hub") {
		// If explicitly set, use the flag value
		// (enableHub is the variable, it's already set by cobra)
	}
	if cmd.Flags().Changed("enable-runtime-host") {
		cfg.RuntimeHost.Enabled = enableRuntimeHost
	}
	if cmd.Flags().Changed("runtime-host-port") {
		cfg.RuntimeHost.Port = runtimeHostPort
	}
	if cmd.Flags().Changed("dev-auth") {
		cfg.Auth.Enabled = enableDevAuth
	}

	// Ensure global directory exists and settings are initialized.
	// This is required for persisting the runtime host identity.
	globalDir, err := config.GetGlobalDir()
	if err != nil {
		return fmt.Errorf("failed to get global directory: %w", err)
	}
	if _, err := os.Stat(globalDir); os.IsNotExist(err) {
		log.Println("Initializing global scion directory...")
		if err := config.InitGlobal(harness.All()); err != nil {
			return fmt.Errorf("failed to initialize global config: %w", err)
		}
	}

	// Check if at least one server is enabled
	if !enableHub && !cfg.RuntimeHost.Enabled {
		return fmt.Errorf("no server components enabled; use --enable-hub or --enable-runtime-host")
	}

	// Check if server ports are already in use
	if enableHub {
		status := checkPort(cfg.Hub.Host, cfg.Hub.Port)
		if status.inUse {
			if status.isScionServer {
				return fmt.Errorf("a scion server is already running on port %d\nUse 'scion server status' to check or 'scion server stop' to stop it", cfg.Hub.Port)
			}
			return fmt.Errorf("Hub port %d is already in use by another process", cfg.Hub.Port)
		}
	}
	if cfg.RuntimeHost.Enabled {
		status := checkPort(cfg.RuntimeHost.Host, cfg.RuntimeHost.Port)
		if status.inUse {
			if status.isScionServer {
				return fmt.Errorf("a scion server is already running on port %d\nUse 'scion server status' to check or 'scion server stop' to stop it", cfg.RuntimeHost.Port)
			}
			return fmt.Errorf("Runtime Host port %d is already in use by another process", cfg.RuntimeHost.Port)
		}
	}

	// Log debug mode status
	if enableDebug {
		log.Println("Debug logging enabled")
		// Log OAuth configuration for debugging
		logOAuthDebug(cfg)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// Initialize store (needed for Hub and for global grove registration)
	var s store.Store
	if enableHub {
		switch cfg.Database.Driver {
		case "sqlite":
			sqliteStore, err := sqlite.New(cfg.Database.URL)
			if err != nil {
				return fmt.Errorf("failed to open database: %w", err)
			}
			s = sqliteStore
			defer s.Close()

			// Run migrations
			if err := s.Migrate(context.Background()); err != nil {
				return fmt.Errorf("failed to run migrations: %w", err)
			}
		default:
			return fmt.Errorf("unsupported database driver: %s", cfg.Database.Driver)
		}

		// Verify database connectivity
		if err := s.Ping(context.Background()); err != nil {
			return fmt.Errorf("database ping failed: %w", err)
		}
	}

	// Variables to track runtime host info for co-located registration
	var hostID string
	var hostName string
	var rt runtime.Runtime
	var hubSrv *hub.Server
	var mgr agent.Manager

	// Initialize dev auth if enabled
	var devAuthToken string
	if cfg.Auth.Enabled {
		globalDir, err := config.GetGlobalDir()
		if err != nil {
			return fmt.Errorf("failed to get global directory: %w", err)
		}

		devAuthCfg := apiclient.DevAuthConfig{
			Enabled:   cfg.Auth.Enabled,
			Token:     cfg.Auth.Token,
			TokenFile: cfg.Auth.TokenFile,
		}

		devAuthToken, err = apiclient.InitDevAuth(devAuthCfg, globalDir)
		if err != nil {
			return fmt.Errorf("failed to initialize dev auth: %w", err)
		}

		log.Println("WARNING: Development authentication enabled - not for production use")
		log.Printf("Dev token: %s", devAuthToken)
		log.Printf("To authenticate CLI commands, run:")
		log.Printf("  export SCION_DEV_TOKEN=%s", devAuthToken)
	}

	// Start Hub API if enabled
	if enableHub {
		// Create Hub server configuration
		hubCfg := hub.ServerConfig{
			Port:               cfg.Hub.Port,
			Host:               cfg.Hub.Host,
			ReadTimeout:        cfg.Hub.ReadTimeout,
			WriteTimeout:       cfg.Hub.WriteTimeout,
			CORSEnabled:        cfg.Hub.CORSEnabled,
			CORSAllowedOrigins: cfg.Hub.CORSAllowedOrigins,
			CORSAllowedMethods: cfg.Hub.CORSAllowedMethods,
			CORSAllowedHeaders: cfg.Hub.CORSAllowedHeaders,
			CORSMaxAge:         cfg.Hub.CORSMaxAge,
			DevAuthToken:       devAuthToken,
			Debug:              enableDebug,
			AuthorizedDomains:  cfg.Auth.AuthorizedDomains,
			OAuthConfig: hub.OAuthConfig{
				Web: hub.OAuthClientConfig{
					Google: hub.OAuthProviderConfig{
						ClientID:     cfg.OAuth.Web.Google.ClientID,
						ClientSecret: cfg.OAuth.Web.Google.ClientSecret,
					},
					GitHub: hub.OAuthProviderConfig{
						ClientID:     cfg.OAuth.Web.GitHub.ClientID,
						ClientSecret: cfg.OAuth.Web.GitHub.ClientSecret,
					},
				},
				CLI: hub.OAuthClientConfig{
					Google: hub.OAuthProviderConfig{
						ClientID:     cfg.OAuth.CLI.Google.ClientID,
						ClientSecret: cfg.OAuth.CLI.Google.ClientSecret,
					},
					GitHub: hub.OAuthProviderConfig{
						ClientID:     cfg.OAuth.CLI.GitHub.ClientID,
						ClientSecret: cfg.OAuth.CLI.GitHub.ClientSecret,
					},
				},
			},
		}

		// Create Hub server
		hubSrv = hub.New(hubCfg, s)

		// Initialize storage if configured
		if storageBucket != "" {
			log.Printf("Initializing GCS storage with bucket: %s", storageBucket)
			storageCfg := storage.Config{
				Provider: storage.ProviderGCS,
				Bucket:   storageBucket,
			}
			stor, err := storage.New(ctx, storageCfg)
			if err != nil {
				return fmt.Errorf("failed to initialize GCS storage: %w", err)
			}
			hubSrv.SetStorage(stor)
			log.Printf("GCS storage configured: gs://%s", storageBucket)
		} else if storageDir != "" {
			log.Printf("Initializing local storage at: %s", storageDir)
			storageCfg := storage.Config{
				Provider:  storage.ProviderLocal,
				LocalPath: storageDir,
			}
			stor, err := storage.New(ctx, storageCfg)
			if err != nil {
				return fmt.Errorf("failed to initialize local storage: %w", err)
			}
			hubSrv.SetStorage(stor)
			log.Printf("Local storage configured: %s", storageDir)
		}

		log.Printf("Starting Hub API server on %s:%d", cfg.Hub.Host, cfg.Hub.Port)
		log.Printf("Database: %s (%s)", cfg.Database.Driver, cfg.Database.URL)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := hubSrv.Start(ctx); err != nil {
				errCh <- fmt.Errorf("hub server error: %w", err)
			}
		}()
	}

	// Start Runtime Host API if enabled
	if cfg.RuntimeHost.Enabled {
		// Initialize runtime (auto-detect based on environment)
		rt = runtime.GetRuntime("", "")

		// Create agent manager
		mgr = agent.NewManager(rt)

		// Load settings to get/persist runtime host identity.
		// The hostID should be durable across server restarts, so we store it in settings.
		settings, err := config.LoadSettings(globalDir)
		if err != nil {
			log.Printf("Warning: failed to load settings: %v", err)
			settings = &config.Settings{}
		}

		// Ensure hub config exists in settings
		if settings.Hub == nil {
			settings.Hub = &config.HubClientConfig{}
		}

		// Get host ID from settings, or generate and persist if not set.
		// Priority: settings.Hub.HostID > cfg.RuntimeHost.HostID > generate new
		hostID = settings.Hub.HostID
		if hostID == "" {
			// Fall back to server config if set
			hostID = cfg.RuntimeHost.HostID
		}
		if hostID == "" {
			// Generate new UUID and persist it
			hostID = api.NewUUID()
			if err := config.UpdateSetting(globalDir, "hub.hostId", hostID, true); err != nil {
				log.Printf("Warning: failed to persist host ID to settings: %v", err)
			} else {
				log.Printf("Generated and persisted new host ID: %s", hostID)
			}
		}

		// Get host nickname from settings, or use hostname as default.
		// Priority: settings.Hub.HostNickname > cfg.RuntimeHost.HostName > os.Hostname()
		hostName = settings.Hub.HostNickname
		if hostName == "" {
			hostName = cfg.RuntimeHost.HostName
		}
		if hostName == "" {
			if hostname, err := os.Hostname(); err == nil {
				hostName = hostname
			} else {
				hostName = "runtime-host"
			}
		}

		// Create Runtime Host server configuration
		rhCfg := runtimehost.ServerConfig{
			Port:               cfg.RuntimeHost.Port,
			Host:               cfg.RuntimeHost.Host,
			ReadTimeout:        cfg.RuntimeHost.ReadTimeout,
			WriteTimeout:       cfg.RuntimeHost.WriteTimeout,
			Mode:               cfg.RuntimeHost.Mode,
			HubEndpoint:        cfg.RuntimeHost.HubEndpoint,
			HostID:             hostID,
			HostName:           hostName,
			CORSEnabled:        cfg.RuntimeHost.CORSEnabled,
			CORSAllowedOrigins: cfg.RuntimeHost.CORSAllowedOrigins,
			CORSAllowedMethods: cfg.RuntimeHost.CORSAllowedMethods,
			CORSAllowedHeaders: cfg.RuntimeHost.CORSAllowedHeaders,
			CORSMaxAge:         cfg.RuntimeHost.CORSMaxAge,
			Debug:              enableDebug,

			// Hub integration for template hydration
			HubEnabled:           cfg.RuntimeHost.HubEndpoint != "",
			HubToken:             devAuthToken, // Use dev token if available
			TemplateCacheDir:     templateCacheDir,
			TemplateCacheMaxSize: templateCacheMax,
		}

		// Create Runtime Host server
		rhSrv := runtimehost.New(rhCfg, mgr, rt)

		log.Printf("Starting Runtime Host API server on %s:%d (mode: %s)",
			cfg.RuntimeHost.Host, cfg.RuntimeHost.Port, cfg.RuntimeHost.Mode)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := rhSrv.Start(ctx); err != nil {
				errCh <- fmt.Errorf("runtime host server error: %w", err)
			}
		}()
	}

	// When both Hub and Runtime Host are enabled together, set up the dispatcher
	// for automatic agent handoff and register the global grove.
	if enableHub && cfg.RuntimeHost.Enabled && s != nil && hubSrv != nil && mgr != nil {
		// Set up the dispatcher to enable automatic agent handoff
		dispatcher := newAgentDispatcherAdapter(mgr, s, hostID)
		hubSrv.SetDispatcher(dispatcher)
		log.Printf("Agent dispatcher configured for co-located runtime host")

		// Register global grove and runtime host
		if err := registerGlobalGroveAndHost(ctx, s, hostID, hostName, rt); err != nil {
			log.Printf("Warning: failed to register global grove: %v", err)
		} else {
			log.Printf("Registered global grove with runtime host %s", hostName)
		}
	}

	// Wait for either an error or context cancellation
	select {
	case err := <-errCh:
		cancel() // Stop other servers
		return err
	case <-ctx.Done():
		// Wait for all servers to shutdown
		wg.Wait()
		return nil
	}
}

// registerGlobalGroveAndHost creates the global grove and registers this
// runtime host as a contributor. This enables automatic agent handoff.
func registerGlobalGroveAndHost(ctx context.Context, s store.Store, hostID, hostName string, rt runtime.Runtime) error {
	// Check if global grove already exists
	globalGrove, err := s.GetGroveBySlug(ctx, GlobalGroveName)
	if err != nil && err != store.ErrNotFound {
		return fmt.Errorf("failed to check for global grove: %w", err)
	}

	// Create global grove if it doesn't exist (without DefaultRuntimeHostID yet)
	groveNeedsDefaultHost := false
	if globalGrove == nil {
		globalGrove = &store.Grove{
			ID:         api.NewUUID(),
			Name:       "Global",
			Slug:       GlobalGroveName,
			Visibility: store.VisibilityPrivate,
			Labels: map[string]string{
				"scion.io/system": "true",
				"scion.io/global": "true",
			},
		}

		if err := s.CreateGrove(ctx, globalGrove); err != nil {
			return fmt.Errorf("failed to create global grove: %w", err)
		}
		groveNeedsDefaultHost = true
	} else if globalGrove.DefaultRuntimeHostID == "" {
		groveNeedsDefaultHost = true
	}

	// Create or update the runtime host record (must happen before setting as default)
	runtimeType := "docker"
	if rt != nil {
		runtimeType = rt.Name()
	}

	host, err := s.GetRuntimeHost(ctx, hostID)
	if err != nil && err != store.ErrNotFound {
		return fmt.Errorf("failed to check for runtime host: %w", err)
	}

	if host == nil {
		host = &store.RuntimeHost{
			ID:              hostID,
			Name:            hostName,
			Slug:            api.Slugify(hostName),
			Mode:            store.HostModeConnected,
			Version:         "0.1.0",
			Status:          store.HostStatusOnline,
			ConnectionState: "connected",
			Capabilities: &store.HostCapabilities{
				WebPTY: false,
				Sync:   true,
				Attach: true,
			},
			Profiles: []store.HostProfile{
				{Name: "default", Type: runtimeType, Available: true},
			},
		}

		if err := s.CreateRuntimeHost(ctx, host); err != nil {
			return fmt.Errorf("failed to create runtime host: %w", err)
		}
	} else {
		// Update existing host status
		host.Status = store.HostStatusOnline
		host.ConnectionState = "connected"
		host.LastHeartbeat = time.Now()
		if err := s.UpdateRuntimeHost(ctx, host); err != nil {
			return fmt.Errorf("failed to update runtime host: %w", err)
		}
	}

	// Now that the runtime host exists, set it as the default for the grove
	if groveNeedsDefaultHost {
		globalGrove.DefaultRuntimeHostID = hostID
		if err := s.UpdateGrove(ctx, globalGrove); err != nil {
			log.Printf("Warning: failed to set default runtime host for global grove: %v", err)
		}
	}

	// Get the global grove path (~/.scion)
	globalPath, err := config.GetGlobalDir()
	if err != nil {
		log.Printf("Warning: failed to get global grove path: %v", err)
		globalPath = "" // Will work but agents may not find the right path
	}

	// Add runtime host as contributor to global grove
	contrib := &store.GroveContributor{
		GroveID:   globalGrove.ID,
		HostID:    hostID,
		HostName:  hostName,
		LocalPath: globalPath, // ~/.scion for the global grove
		Mode:      store.HostModeConnected,
		Status:    store.HostStatusOnline,
		LastSeen:  time.Now(),
	}

	if err := s.AddGroveContributor(ctx, contrib); err != nil {
		// Ignore duplicate contributor errors
		if err != store.ErrAlreadyExists {
			return fmt.Errorf("failed to add grove contributor: %w", err)
		}
		// Update contributor status
		if err := s.UpdateContributorStatus(ctx, globalGrove.ID, hostID, store.HostStatusOnline); err != nil {
			log.Printf("Warning: failed to update contributor status: %v", err)
		}
	}

	return nil
}

// agentDispatcherAdapter adapts the agent.Manager to the hub.AgentDispatcher interface.
// This enables the Hub to dispatch agent creation to a co-located runtime host.
type agentDispatcherAdapter struct {
	manager agent.Manager
	store   store.Store
	hostID  string // The ID of this runtime host
}

// newAgentDispatcherAdapter creates a new dispatcher adapter.
func newAgentDispatcherAdapter(mgr agent.Manager, s store.Store, hostID string) *agentDispatcherAdapter {
	return &agentDispatcherAdapter{
		manager: mgr,
		store:   s,
		hostID:  hostID,
	}
}

// DispatchAgentCreate implements hub.AgentDispatcher.
// It starts the agent on the runtime host and updates the hub store with runtime info.
func (d *agentDispatcherAdapter) DispatchAgentCreate(ctx context.Context, hubAgent *store.Agent) error {
	// Look up the local path for this grove on this runtime host
	var grovePath string
	if hubAgent.GroveID != "" && d.hostID != "" {
		contrib, err := d.store.GetGroveContributor(ctx, hubAgent.GroveID, d.hostID)
		if err != nil {
			log.Printf("Warning: failed to get grove contributor for path lookup: %v", err)
		} else if contrib.LocalPath != "" {
			grovePath = contrib.LocalPath
		}
	}

	// Build StartOptions from the hub agent record
	env := make(map[string]string)
	if hubAgent.AppliedConfig != nil && hubAgent.AppliedConfig.Env != nil {
		env = hubAgent.AppliedConfig.Env
	}

	// Add grove ID label for tracking
	if hubAgent.Labels == nil {
		hubAgent.Labels = make(map[string]string)
	}
	hubAgent.Labels["scion.grove"] = hubAgent.GroveID

	opts := api.StartOptions{
		Name:      hubAgent.Name,
		Template:  hubAgent.Template,
		Image:     hubAgent.Image,
		Env:       env,
		Detached:  &hubAgent.Detached,
		GrovePath: grovePath, // Pass the local filesystem path for this grove
	}

	if hubAgent.AppliedConfig != nil {
		opts.Template = hubAgent.AppliedConfig.Harness
		// Pass the task through to the runtime host
		if hubAgent.AppliedConfig.Task != "" {
			opts.Task = hubAgent.AppliedConfig.Task
		}
	}

	// Start the agent on the runtime host
	agentInfo, err := d.manager.Start(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	// Update the hub agent record with runtime information
	hubAgent.Status = store.AgentStatusRunning
	hubAgent.ContainerStatus = agentInfo.ContainerStatus
	if agentInfo.ID != "" {
		hubAgent.RuntimeState = "container:" + agentInfo.ID
	}
	hubAgent.LastSeen = time.Now()

	if err := d.store.UpdateAgent(ctx, hubAgent); err != nil {
		log.Printf("Warning: failed to update agent with runtime info: %v", err)
	}

	return nil
}

// DispatchAgentStart implements hub.AgentDispatcher.
// For co-located runtime hosts, this resumes a stopped agent.
func (d *agentDispatcherAdapter) DispatchAgentStart(ctx context.Context, hubAgent *store.Agent) error {
	// For now, starting an existing agent is not fully supported in the manager
	// The manager's Start method creates new agents, not resumes existing ones
	// TODO: Implement proper agent resume functionality in the manager
	log.Printf("DispatchAgentStart called for agent %s (not fully implemented)", hubAgent.Name)
	return nil
}

// DispatchAgentStop implements hub.AgentDispatcher.
// It stops a running agent on the runtime host.
func (d *agentDispatcherAdapter) DispatchAgentStop(ctx context.Context, hubAgent *store.Agent) error {
	if err := d.manager.Stop(ctx, hubAgent.Name); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	// Update the hub agent record
	hubAgent.Status = store.AgentStatusStopped
	hubAgent.LastSeen = time.Now()

	if err := d.store.UpdateAgent(ctx, hubAgent); err != nil {
		log.Printf("Warning: failed to update agent status: %v", err)
	}

	return nil
}

// DispatchAgentRestart implements hub.AgentDispatcher.
// It restarts an agent on the runtime host.
func (d *agentDispatcherAdapter) DispatchAgentRestart(ctx context.Context, hubAgent *store.Agent) error {
	// Stop then start
	if err := d.manager.Stop(ctx, hubAgent.Name); err != nil {
		log.Printf("Warning: failed to stop agent during restart: %v", err)
	}

	// TODO: Implement proper restart with start after stop
	// For now, just update status
	hubAgent.Status = store.AgentStatusRunning
	hubAgent.LastSeen = time.Now()

	if err := d.store.UpdateAgent(ctx, hubAgent); err != nil {
		log.Printf("Warning: failed to update agent status: %v", err)
	}

	return nil
}

// DispatchAgentDelete implements hub.AgentDispatcher.
// It removes an agent from the runtime host.
func (d *agentDispatcherAdapter) DispatchAgentDelete(ctx context.Context, hubAgent *store.Agent, deleteFiles, removeBranch bool) error {
	// Look up the local path for this grove on this runtime host
	var grovePath string
	if hubAgent.GroveID != "" && d.hostID != "" {
		contrib, err := d.store.GetGroveContributor(ctx, hubAgent.GroveID, d.hostID)
		if err != nil {
			log.Printf("Warning: failed to get grove contributor for path lookup: %v", err)
		} else if contrib.LocalPath != "" {
			grovePath = contrib.LocalPath
		}
	}

	// Stop the agent first (ignore error if already stopped)
	_ = d.manager.Stop(ctx, hubAgent.Name)

	// Delete the agent
	_, err := d.manager.Delete(ctx, hubAgent.Name, deleteFiles, grovePath, removeBranch)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	return nil
}

// DispatchAgentMessage implements hub.AgentDispatcher.
// It sends a message to an agent on the runtime host.
func (d *agentDispatcherAdapter) DispatchAgentMessage(ctx context.Context, hubAgent *store.Agent, message string, interrupt bool) error {
	if err := d.manager.Message(ctx, hubAgent.Name, message, interrupt); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	return nil
}

// logOAuthDebug logs OAuth configuration details for debugging.
// Secrets are redacted to only show whether they are set.
func logOAuthDebug(cfg *config.GlobalConfig) {
	log.Println("[Debug] OAuth Configuration:")
	log.Printf("[Debug]   CLI Google ClientID: %s", redactForDebug(cfg.OAuth.CLI.Google.ClientID))
	log.Printf("[Debug]   CLI Google ClientSecret: %s", redactForDebug(cfg.OAuth.CLI.Google.ClientSecret))
	log.Printf("[Debug]   CLI GitHub ClientID: %s", redactForDebug(cfg.OAuth.CLI.GitHub.ClientID))
	log.Printf("[Debug]   CLI GitHub ClientSecret: %s", redactForDebug(cfg.OAuth.CLI.GitHub.ClientSecret))
	log.Printf("[Debug]   Web Google ClientID: %s", redactForDebug(cfg.OAuth.Web.Google.ClientID))
	log.Printf("[Debug]   Web Google ClientSecret: %s", redactForDebug(cfg.OAuth.Web.Google.ClientSecret))
	log.Printf("[Debug]   Web GitHub ClientID: %s", redactForDebug(cfg.OAuth.Web.GitHub.ClientID))
	log.Printf("[Debug]   Web GitHub ClientSecret: %s", redactForDebug(cfg.OAuth.Web.GitHub.ClientSecret))
}

// redactForDebug returns a redacted version of a secret for debug logging.
func redactForDebug(value string) string {
	if value == "" {
		return "(not set)"
	}
	if len(value) <= 8 {
		return "(set, " + fmt.Sprintf("%d", len(value)) + " chars)"
	}
	return value[:4] + "..." + value[len(value)-4:] + " (" + fmt.Sprintf("%d", len(value)) + " chars)"
}

func init() {
	rootCmd.AddCommand(serverCmd)
	serverCmd.AddCommand(serverStartCmd)

	// Server start flags
	serverStartCmd.Flags().StringVarP(&serverConfigPath, "config", "c", "", "Path to server configuration file")

	// Hub API flags
	serverStartCmd.Flags().BoolVar(&enableHub, "enable-hub", false, "Enable the Hub API")
	serverStartCmd.Flags().IntVar(&hubPort, "port", 9810, "Hub API port")
	serverStartCmd.Flags().StringVar(&hubHost, "host", "0.0.0.0", "Hub API host to bind")
	serverStartCmd.Flags().StringVar(&dbURL, "db", "", "Database URL/path")

	// Runtime Host API flags
	serverStartCmd.Flags().BoolVar(&enableRuntimeHost, "enable-runtime-host", false, "Enable the Runtime Host API")
	serverStartCmd.Flags().IntVar(&runtimeHostPort, "runtime-host-port", 9800, "Runtime Host API port")

	// Auth flags
	serverStartCmd.Flags().BoolVar(&enableDevAuth, "dev-auth", false, "Enable development authentication (auto-generates token)")

	// Debug flags
	serverStartCmd.Flags().BoolVar(&enableDebug, "debug", false, "Enable debug logging (verbose output)")

	// Storage flags
	serverStartCmd.Flags().StringVar(&storageBucket, "storage-bucket", "", "GCS bucket name for template storage")
	serverStartCmd.Flags().StringVar(&storageDir, "storage-dir", "", "Local directory for template storage (alternative to GCS)")

	// Template cache flags (for Runtime Host)
	serverStartCmd.Flags().StringVar(&templateCacheDir, "template-cache-dir", "", "Directory for caching templates from Hub (default: ~/.scion/cache/templates)")
	serverStartCmd.Flags().Int64Var(&templateCacheMax, "template-cache-max", 100*1024*1024, "Maximum template cache size in bytes (default: 100MB)")
}
