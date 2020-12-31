package main

import (
    "flag"
    "fmt"
    "os"
    "github.com/shirou/gopsutil/process"
)

type Config struct {
    uri                     string
    bindAddress             string
    fileIndex               int
    maxUploadRate           int
    maxDownloadRate         int
    connectionsLimit        int
    downloadPath            string
    resumeFile              string
    stateFile               string
    userAgent               string
    keepComplete            bool
    keepIncomplete          bool
    keepFiles               bool
    encryption              int
    noSparseFile            bool
    idleTimeout             int
    peerConnectTimeout      int
    requestTimeout     		int
    torrentConnectBoost     int
    connectionSpeed         int
    listenPort              int
    minReconnectTime        int
    maxFailCount            int
    randomPort              bool
    debugAlerts				bool
    showAllStats            bool
    showOverallProgress     bool
    showFilesProgress       bool
    showPiecesProgress      bool
    enableScrape            bool
    enableDHT               bool
    enableLSD               bool
    enableUPNP              bool
    enableNATPMP            bool
    enableUTP               bool
    enableTCP               bool
    exitOnFinish			bool
    dhtRouters              string
    trackers                string
    tunedStorage            bool
    cmdlineProc             string
    downloadStorage         int
}

func (c Config) parseFlags() {
    config = Config{}
    flag.StringVar(&config.uri, "uri", "", "Magnet URI or .torrent file URL")
    flag.StringVar(&config.bindAddress, "bind", "localhost:5001", "Bind address of torrent2http")
    flag.StringVar(&config.downloadPath, "dl-path", ".", "Download path")
    flag.IntVar(&config.idleTimeout, "max-idle", -1, "Automatically shutdown if no connection are active after a timeout")
    flag.IntVar(&config.fileIndex, "file-index", -1, "Start downloading file with specified index immediately (or start in paused state otherwise)")
    flag.BoolVar(&config.keepComplete, "keep-complete", false, "Keep complete files after exiting")
    flag.BoolVar(&config.keepIncomplete, "keep-incomplete", false, "Keep incomplete files after exiting")
    flag.BoolVar(&config.keepFiles, "keep-files", false, "Keep all files after exiting (incl. -keep-complete and -keep-incomplete)")
    flag.BoolVar(&config.showAllStats, "show-stats", false, "Show all stats (incl. -overall-progress -files-progress -pieces-progress)")
    flag.BoolVar(&config.showOverallProgress, "overall-progress", false, "Show overall progress")
    flag.BoolVar(&config.showFilesProgress, "files-progress", false, "Show files progress")
    flag.BoolVar(&config.showPiecesProgress, "pieces-progress", false, "Show pieces progress")
    flag.BoolVar(&config.debugAlerts, "debug-alerts", false, "Show debug alert notifications")
    flag.BoolVar(&config.exitOnFinish, "exit-on-finish", false, "Exit when download finished")

    flag.StringVar(&config.resumeFile, "resume-file", "", "Use fast resume file")
    flag.StringVar(&config.stateFile, "state-file", "", "Use file for saving/restoring session state")
    flag.StringVar(&config.userAgent, "user-agent", USER_AGENT, "Set an user agent")
    flag.StringVar(&config.dhtRouters, "dht-routers", "", "Additional DHT routers (comma-separated host:port pairs)")
    flag.StringVar(&config.trackers, "trackers", "", "Additional trackers (comma-separated URLs)")
    flag.IntVar(&config.listenPort, "listen-port", 6881, "Use specified port for incoming connections")
    flag.IntVar(&config.torrentConnectBoost, "torrent-connect-boost", 50, "The number of peers to try to connect to immediately when the first tracker response is received for a torrent")
    flag.IntVar(&config.connectionSpeed, "connection-speed", 50, "The number of peer connection attempts that are made per second")
    flag.IntVar(&config.peerConnectTimeout, "peer-connect-timeout", 15, "The number of seconds to wait after a connection attempt is initiated to a peer")
    flag.IntVar(&config.requestTimeout, "request-timeout", 20, "The number of seconds until the current front piece request will time out")
    flag.IntVar(&config.maxDownloadRate, "dl-rate", -1, "Max download rate (kB/s)")
    flag.IntVar(&config.maxUploadRate, "ul-rate", -1, "Max upload rate (kB/s)")
    flag.IntVar(&config.connectionsLimit, "connections-limit", 200, "Set a global limit on the number of connections opened")
    flag.IntVar(&config.encryption, "encryption", 1, "Encryption: 0=forced 1=enabled (default) 2=disabled")
    flag.IntVar(&config.minReconnectTime, "min-reconnect-time", 60, "The time to wait between peer connection attempts. If the peer fails, the time is multiplied by fail counter")
    flag.IntVar(&config.maxFailCount, "max-failcount", 3, "The maximum times we try to connect to a peer before stop connecting again")
    flag.BoolVar(&config.noSparseFile, "no-sparse", false, "Do not use sparse file allocation")
    flag.BoolVar(&config.randomPort, "random-port", false, "Use random listen port (49152-65535)")
    flag.BoolVar(&config.enableScrape, "enable-scrape", false, "Enable sending scrape request to tracker (updates total peers/seeds count)")
    flag.BoolVar(&config.enableDHT, "enable-dht", true, "Enable DHT (Distributed Hash Table)")
    flag.BoolVar(&config.enableLSD, "enable-lsd", true, "Enable LSD (Local Service Discovery)")
    flag.BoolVar(&config.enableUPNP, "enable-upnp", true, "Enable UPnP (UPnP port-mapping)")
    flag.BoolVar(&config.enableNATPMP, "enable-natpmp", true, "Enable NATPMP (NAT port-mapping)")
    flag.BoolVar(&config.enableUTP, "enable-utp", true, "Enable uTP protocol")
    flag.BoolVar(&config.enableTCP, "enable-tcp", true, "Enable TCP protocol")
    flag.BoolVar(&config.tunedStorage, "tuned-storage", false, "Enable storage optimizations for Android external storage / OS-mounted NAS setups")
    flag.StringVar(&config.cmdlineProc, "cmdline-proc", "", "Display cmdline of specified process")
    flag.IntVar(&config.downloadStorage, "down-storage", 0, "Download storage: 0=file storage 1=ram memory")
    flag.Parse()

    if config.uri == "" || config.cmdlineProc != "" {
        if config.cmdlineProc != "" {
            cmdlinep := ProcessTable(config.cmdlineProc)
            for k,_ := range cmdlinep {
                fmt.Println(cmdlinep[k])
            }
            os.Exit(0)
        }
        flag.Usage()
        os.Exit(1)
    }
    if config.resumeFile != "" && !config.keepFiles {
        fmt.Println("Usage of option -resume-file is allowed only along with -keep-files")
        os.Exit(1)
    }
}

//Returns the command line for a given process name
func ProcessTable(str string) map[int32]string {
    res := make(map[int32]string)
    pids, _ := process.Pids()

    for _, pid := range pids {
        proc, _ := process.NewProcess(pid)
        name, _ := proc.Name()
        if name == str {
            cline, _ := proc.Cmdline()
            res[pid] = cline
        }
    }
    return res
}
