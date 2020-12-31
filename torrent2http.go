package main

import (
    "encoding/json"
    "encoding/hex"
    "io/ioutil"
    "math/rand"
    "fmt"
    "log"
    "net/http"
    "net/url"
    "os"
    "net"
    "os/signal"
    "path/filepath"
    "runtime"
    "syscall"
    "strings"
    "strconv"
    "time"

    lt "github.com/ElementumOrg/libtorrent-go"
    "github.com/saintfish/chardet"
    "golang.org/x/net/html/charset"
    "golang.org/x/text/transform"
)

type FileStatusInfo struct {
    Name        string  `json:"name"`
    SavePath    string  `json:"save_path"`
    Url         string  `json:"url"`
    Size        int64   `json:"size"`
    Offset      int64   `json:"offset"`
    Download    int64   `json:"download"`
    Progress    float32 `json:"progress"`
    Priority    int  `json:"priority"`
}

type LsInfo struct {
    Files          []FileStatusInfo `json:"files"`
}

type PeerInfo struct {
    Ip             string  `json:"ip"`
    Flags          uint    `json:"flags"`
    Source         uint     `json:"source"`
    UpSpeed        float32 `json:"up_speed"`
    DownSpeed      float32 `json:"down_speed"`
    TotalUpload    int64   `json:"total_upload"`
    TotalDownload  int64   `json:"total_download"`
    Country        string  `json:"country"`
    Client         string  `json:"client"`
}

type PeersInfo struct {
    Peers          []PeerInfo `json:"peers"`
}

type TrackerInfo struct {
    Url            string  `json:"url"`
    NextAnnounceIn int     `json:"next_announce_in"`
    MinAnnounceIn  int     `json:"min_announce_in"`
    ErrorCode      int     `json:"error_code"`
    ErrorMessage   string  `json:"error_message"`
    Message        string  `json:"message"`
    Tier           byte    `json:"tier"`
    FailLimit      byte    `json:"fail_limit"`
    Fails          byte    `json:"fails"`
    Source         byte    `json:"source"`
    Verified       bool    `json:"verified"`
    Updating       bool    `json:"updating"`
    StartSent      bool    `json:"start_sent"`
    CompleteSent   bool    `json:"complete_sent"`
}

type TrackersInfo struct {
    Trackers       []TrackerInfo `json:"trackers"`
}

type SessionStatus struct {
    Name           string   `json:"name"`
    State          int      `json:"state"`
    StateStr       string   `json:"state_str"`
    Error          string   `json:"error"`
    Progress       float32  `json:"progress"`
    DownloadRate   float64  `json:"download_rate"`
    UploadRate     float64  `json:"upload_rate"`
    TotalDownload  int64    `json:"total_download"`
    TotalUpload    int64    `json:"total_upload"`
    NumPeers       int      `json:"num_peers"`
    NumSeeds       int      `json:"num_seeds"`
    TotalSeeds     int      `json:"total_seeds"`
    TotalPeers     int      `json:"total_peers"`
    HashString     string   `json:"hash_string"`
    SessionStat    string   `json:"session_status"`
}

const VERSION = "1.1.14"
const USER_AGENT = "torrent2http/"+VERSION+" libtorrent/"+lt.LIBTORRENT_VERSION

const (
    ipToSDefault     = iota
    ipToSLowDelay    = 1 << iota
    ipToSReliability = 1 << iota
    ipToSThroughput  = 1 << iota
    ipToSlowCost     = 1 << iota
)
var (
    config Config
    session lt.SessionHandle
    sessionglobal lt.Session
    torrentHandle lt.TorrentHandle
    torrentFS *TorrentFS
    forceShutdown chan bool
    httpListener net.Listener
    PackSettings  lt.SettingsPack
    mappedPorts map[string]int
)

const (
    STATE_QUEUED_FOR_CHECKING = iota
    STATE_CHECKING_FILES
    STATE_DOWNLOADING_METADATA
    STATE_DOWNLOADING
    STATE_FINISHED
    STATE_SEEDING
    STATE_ALLOCATING
    STATE_CHECKING_RESUME_DATA
)

const (
	// ProxyTypeNone ...
	ProxyTypeNone = iota
	// ProxyTypeSocks4 ...
	ProxyTypeSocks4
	// ProxyTypeSocks5 ...
	ProxyTypeSocks5
	// ProxyTypeSocks5Password ...
	ProxyTypeSocks5Password
	// ProxyTypeSocksHTTP ...
	ProxyTypeSocksHTTP
	// ProxyTypeSocksHTTPPassword ...
	ProxyTypeSocksHTTPPassword
	// ProxyTypeI2PSAM ...
	ProxyTypeI2PSAM
)

var dhtBootstrapNodes = []string{
	"router.bittorrent.com:6881",
	"router.utorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"dht.aelitis.com:6881",     // Vuze
	"dht.libtorrent.org:25401", // Libtorrent
}

// DefaultTrackers ...
var DefaultTrackers = []string{
	"http://bt4.t-ru.org/ann?magnet",
	"http://retracker.mgts.by:80/announce",
	"http://tracker.city9x.com:2710/announce",
	"http://tracker.electro-torrent.pl:80/announce",
	"http://tracker.internetwarriors.net:1337/announce",
	"http://bt.svao-ix.ru/announce",

	"udp://tracker.opentrackr.org:1337/announce",
	"udp://tracker.coppersurfer.tk:6969/announce",
	"udp://tracker.leechers-paradise.org:6969/announce",
	"udp://tracker.openbittorrent.com:80/announce",
	"udp://public.popcorn-tracker.org:6969/announce",
	"udp://explodie.org:6969",
	"udp://opentor.org:2710",
}

var stateStrings = map[int]string{
    STATE_QUEUED_FOR_CHECKING: "queued_for_checking",
    STATE_CHECKING_FILES: "checking_files",
    STATE_DOWNLOADING_METADATA: "downloading_metadata",
    STATE_DOWNLOADING: "downloading",
    STATE_FINISHED: "finished",
    STATE_SEEDING: "seeding",
    STATE_ALLOCATING: "allocating",
    STATE_CHECKING_RESUME_DATA: "checking_resume_data",
}

func convertToUtf8(s string) (string) {
    b := []byte(s)
    d := chardet.NewTextDetector()
    r, err := d.DetectBest(b)
    if err != nil {
        return fmt.Sprintf("<Can't detect string charset: %s>", err.Error())
    }
    encoding, _ := charset.Lookup(r.Charset)
    if encoding == nil {
        return fmt.Sprintf("<Can't find encoding: %s>", r.Charset)
    }
    str, _, err := transform.String(encoding.NewDecoder(), s)
    if err != nil {
        return fmt.Sprintf("<Can't convert string from encoding %s to UTF8: %s>", r.Charset, err.Error())
    }
    return str
}

func statusHandler(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    var status SessionStatus
    var statsesion string
    
    if torrentHandle == nil {
        status = SessionStatus{State: -1}
    } else {
        tstatus := torrentHandle.Status()
        if session.IsPaused() {
            statsesion = "paused"
        } else {
            statsesion = "running"
        }
        status = SessionStatus{
            Name:          tstatus.GetName(),
            State:         int(tstatus.GetState()),
            StateStr:	   stateStrings[int(tstatus.GetState())],
            Error:         tstatus.GetError(),
            Progress:      tstatus.GetProgress(),
            TotalDownload: tstatus.GetTotalDownload(),
            TotalUpload:   tstatus.GetTotalUpload(),
            DownloadRate:  float64(tstatus.GetDownloadPayloadRate()) / 1024,
            UploadRate:    float64(tstatus.GetUploadPayloadRate()) / 1024,
            NumPeers:      tstatus.GetNumPeers(),
            TotalPeers:    tstatus.GetListPeers(),
            NumSeeds:      tstatus.GetNumSeeds(),
            TotalSeeds:    tstatus.GetListSeeds(),
            HashString:    hex.EncodeToString([]byte(tstatus.GetInfoHash().ToString())),
            SessionStat:   statsesion}
    }

    output, _ := json.Marshal(status)
    w.Write(output)
}

func stats() {
    status := torrentHandle.Status()
    if !status.GetHasMetadata() {
        return
    }
    if config.showAllStats || config.showOverallProgress {
        sessionStatus := session.Status()
        dhtStatusStr := ""
        if session.IsDhtRunning() {
            dhtStatusStr = fmt.Sprintf(", DHT nodes: %d", sessionStatus.GetDhtNodes())
        }
        errorStr := ""
        if len(status.GetError()) > 0 {
            errorStr = fmt.Sprintf(" (%s)", status.GetError())
        }
        log.Printf("%s, overall progress: %.2f%%, dl/ul: %.3f/%.3f kbps, peers/seeds: %d/%d" + dhtStatusStr + errorStr,
            strings.Title(stateStrings[int(status.GetState())]),
                status.GetProgress()*100,
                float32(status.GetDownloadRate())/1024,
                float32(status.GetUploadRate())/1024,
            status.GetNumPeers(),
            status.GetNumSeeds(),
        )
    }
    if config.showFilesProgress || config.showAllStats {
        str := "Files: "
        for i, f := range torrentFS.Files() {
            str += fmt.Sprintf("[%d] %.2f%% ", i, f.Progress()*100)
        }
        log.Println(str)
    }
    if (config.showPiecesProgress || config.showAllStats) && torrentFS.LastOpenedFile() != nil {
        torrentFS.LastOpenedFile().ShowPieces()
    }
}

func lsHandler(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    retFiles := LsInfo{}
    
    if torrentFS.HasTorrentInfo() {
        for index, file := range torrentFS.Files() {
            prio := 0
            for indexf, priority := range torrentFS.priorities {
                if indexf == index {
                    prio = priority
                    break
                }
            }
            url := url.URL{
                Scheme:    "http",
                Host:      config.bindAddress,
                Path:      "/files/" + file.Name(),
            }
            fi := FileStatusInfo{
                Name:      file.Name(),
                Size:      file.Size(),
                Offset:    file.Offset(),
                Download:  file.Downloaded(),
                Progress:  file.Progress(),
                SavePath:  file.SavePath(),
                Url:       url.String(),
                Priority:  prio,
            }
            retFiles.Files = append(retFiles.Files, fi)
        }
    }

    output, _ := json.Marshal(retFiles)
    w.Write(output)
}

func peersHandler(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    ret := PeersInfo{}

    vectorPeerInfo := lt.NewStdVectorPeerInfo()
    torrentHandle.GetPeerInfo(vectorPeerInfo)
    for i := 0; i < int(vectorPeerInfo.Size()); i++ {
        peer := vectorPeerInfo.Get(i)
        pi := PeerInfo{
            Ip:              fmt.Sprint(peer.GetIp()),
            Flags:           peer.GetFlags(),
            Source:          peer.GetSource(),
            UpSpeed:         float32(peer.GetUpSpeed())/1024,
            DownSpeed:       float32(peer.GetDownSpeed())/1024,
            TotalDownload:   peer.GetTotalDownload(),
            TotalUpload:     peer.GetTotalUpload(),
            Country:         peer.GetCountry(),
            Client:          peer.GetClient(),
        }
        ret.Peers = append(ret.Peers, pi)
    }

    output, _ := json.Marshal(ret)
    w.Write(output)
}

func trackersHandler(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    ret := TrackersInfo{}

    vectorAnnounceEntry := torrentHandle.Trackers()
    for i := 0; i < int(vectorAnnounceEntry.Size()); i++ {
        entry := vectorAnnounceEntry.Get(i)
        pi := TrackerInfo{
            Url:				entry.GetUrl(),
            NextAnnounceIn:		entry.NextAnnounceIn(),
            MinAnnounceIn:		entry.MinAnnounceIn(),
            ErrorCode:			entry.GetLastError().Value(),
            ErrorMessage:		convertToUtf8(entry.GetLastError().Message().(string)),
            Message:			convertToUtf8(entry.GetMessage()),
            Tier:				entry.GetTier(),
            FailLimit:			entry.GetFailLimit(),
            Fails:				entry.GetFails(),
            Source:				entry.GetSource(),
            Verified:			entry.GetVerified(),
            Updating:			entry.GetUpdating(),
            StartSent:			entry.GetStartSent(),
            CompleteSent:		entry.GetCompleteSent(),
        }
        ret.Trackers = append(ret.Trackers, pi)
    }

    output, _ := json.Marshal(ret)
    w.Write(output)
}

func filesToRemove() []string {
    var files []string
    if torrentFS.HasTorrentInfo() {
        for _, file := range torrentFS.Files() {
            if (!config.keepComplete || !file.IsComplete()) && (!config.keepIncomplete || file.IsComplete()) {
                if _, err := os.Stat(file.SavePath()); !os.IsNotExist(err) {
                    files = append(files, file.SavePath())
                }
            }
        }
    }
    return files
}

func trimPathSeparator(path string) string {
    last := len(path)-1
    if last > 0 && os.IsPathSeparator(path[last]) {
        path = path[:last]
    }
    return path
}

func removeFiles(files [] string) {
    for _, file := range files {
        if err := os.Remove(file); err != nil {
            log.Println(err)
        } else {
            // Remove empty folders as well
            path := filepath.Dir(file)
            savePath, _ := filepath.Abs(config.downloadPath)
            savePath = trimPathSeparator(savePath)
            for path != savePath {
                os.Remove(path)
                path = trimPathSeparator(filepath.Dir(path))
            }
            log.Println("files removed...")
        }
    }
}

func waitForAlert(name string, timeout time.Duration) lt.Alert {
    start := time.Now()
    for {
        for {
            alert := session.WaitForAlert(lt.Milliseconds(100))
            if time.Now().Sub(start) > timeout {
                return nil
            }
            if alert.Swigcptr() != 0 {
                alert = popAlert(false)
                //log.Printf("waiting for alert (%s) %s", alert.What(), alert.Message())
                if alert.What() == name {
                    return alert
                }
            }
        }
    }
}

func removeTorrent() {
    var flag int
    var files []string

    state := torrentHandle.Status().GetState()
    if state != STATE_CHECKING_FILES && state != STATE_QUEUED_FOR_CHECKING && !config.keepFiles {
        if !config.keepComplete && !config.keepIncomplete {
            flag = int(lt.SessionHandleDeleteFiles)
        } else {
            files = filesToRemove()
        }
    }
    log.Println("Removing the torrent")
    session.RemoveTorrent(torrentHandle, flag)
    if flag != 0 || len(files) > 0 {
        log.Println("Waiting for files to be removed")
        waitForAlert("torrent_removed_alert", 15*time.Second)
        removeFiles(files)
    }
}

func saveResumeData(async bool) bool {
    if !torrentHandle.Status().GetNeedSaveResume() || config.resumeFile == "" {
        return false
    }
    torrentHandle.SaveResumeData(3)
    if !async {
        alert := waitForAlert("save_resume_data_alert", 5*time.Second)
        if alert == nil {
            return false
        }
        processSaveResumeDataAlert(alert)
    }
    return true
}

func saveSessionState() {
    if config.stateFile == "" {
        return
    }
    entry := lt.NewEntry()
    session.SaveState(entry)
    data := lt.Bencode(entry)
    log.Printf("Saving session state to: %s", config.stateFile)
    err := ioutil.WriteFile(config.stateFile, []byte(data), 0644)
    if err != nil {
        log.Println(err)
    }
}

func shutdown() {
    log.Println("Stopping torrent2http...")
    if session != nil {
        if !session.IsPaused() {
            session.Pause()
            waitForAlert("torrent_paused_alert", 10*time.Second)
        }
        if torrentHandle != nil {
            saveResumeData(false)
            saveSessionState()
            removeTorrent()
        }
        log.Println("Aborting the session")
        lt.DeleteSession(sessionglobal)
    }
    log.Println("Bye bye")
}

func NewConnectionCounterHandler(connTrackChannel chan int, handler http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        connTrackChannel <- 1
        handler.ServeHTTP(w, r)
        connTrackChannel <- -1
    })
}

func inactiveAutoShutdown(connTrackChannel chan int) {
    activeConnections := 0

    for {
        if activeConnections == 0 {
            select {
            case inc := <-connTrackChannel:
                activeConnections += inc
            case <-time.After(time.Duration(config.idleTimeout) * time.Second):
                forceShutdown <- true
            }
        } else {
            activeConnections += <-connTrackChannel
        }
    }
}

func getHandler(h http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        index, err := strconv.Atoi(r.URL.String())
        if err == nil && torrentFS.HasTorrentInfo() {
            file, err := torrentFS.FileAt(index)
            if err == nil {
                r.URL.Path = file.Name()
                h.ServeHTTP(w, r)
                return
            }
        }
        http.NotFound(w, r)
    })
}

func prioHandler(w http.ResponseWriter, r *http.Request) {
    
    var name string
    
    query := r.URL.Query()
    index, err := strconv.Atoi(query.Get("index"))
    priority, err := strconv.Atoi(query.Get("priority"))
    torrentFS.setPriority(index, priority)
    file, err := torrentFS.FileAt(index)
    if err == nil {
        name = file.Name()
    }
    ret := "File named: " + name + " is set with priority " + strconv.Itoa(priority)
    
    w.WriteHeader(200)
    w.Write([]byte(ret))
}

func comandHandler(w http.ResponseWriter, r *http.Request) {
    
    settings := PackSettings
    query := r.URL.Query()
    ret := ""
    confcom, ok := query["command"]
    if !ok || len(confcom) == 0 {
        log.Println("url param command missing")
    }
    confmode, ok := query["mode"]
    if !ok || len(confmode) == 0 {
        log.Println("url param mode missing")
    }
    confval, ok := query["value"]
    if !ok || len(confval) == 0 {
        log.Println("url param value missing")
    }
    if len(confcom) == len(confmode) && len(confcom) == len(confval) {
        for i := 0; i < len(confcom) -1; i++ {
            if confmode[i] == "bool" {
                settings.SetBool(confcom[i], confval[i] == "true")
                ret += "command: " + confcom[i] + ": " + confval[i] + " executed \n"
            } else {
                cint, err := strconv.Atoi(confval[i])
                if err == nil {
                    settings.SetInt(confcom[i], cint)
                    ret += "command: " + confcom[i] + ": " + confval[i] + " executed \n"
                }
            }
        }
    }
    session.ApplySettings(settings)

    w.WriteHeader(200)
    w.Write([]byte(ret))
}

func stopanddelete() {
    log.Println("Stopping torrent2http...")
    if session != nil {
        if !session.IsPaused() {
            session.Pause()
        }
        if torrentHandle != nil {
            forceremoveTorrent()
        }
    }
    log.Println("Bye bye")
}

func forceremoveTorrent() {
    var flag int
    var files []string
    flag = int(lt.SessionHandleDeleteFiles)
    files = forcefilesToRemove()
    log.Println("Removing the torrent")
    session.RemoveTorrent(torrentHandle, flag)
    if flag != 0 || len(files) > 0 {
        log.Println("Waiting for files to be removed")
        removeFiles(files)
    }
}

func forcefilesToRemove() []string {
    var files []string
    if torrentFS.HasTorrentInfo() {
        for _, file := range torrentFS.Files() {
            if _, err := os.Stat(file.SavePath()); !os.IsNotExist(err) {
                files = append(files, file.SavePath())
            }
        }
    }
    return files
}

func startHTTP() {
    log.Println("Starting HTTP Server...")

    mux := http.NewServeMux()
    mux.HandleFunc("/status", statusHandler)
    mux.HandleFunc("/ls", lsHandler)
    mux.HandleFunc("/peers", peersHandler)
    mux.HandleFunc("/trackers", trackersHandler)
    mux.HandleFunc("/command", comandHandler)
    mux.Handle("/get/", http.StripPrefix("/get/", getHandler(http.FileServer(torrentFS))))
    mux.HandleFunc("/priority", prioHandler)
    mux.HandleFunc("/stopanddelete", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "torrent stopped and files deleted")
        stopanddelete()
        forceShutdown <- true
        torrentFS.Shutdown()
        lt.DeleteSession(sessionglobal)
        os.Exit(0)
    })
    mux.HandleFunc("/shutdown", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "OK")
        forceShutdown <- true
    })
    mux.HandleFunc("/stop", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "Torrent Stopped")
        session.Pause()
    })
    mux.HandleFunc("/pausetorrent", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "Torrent Paused")
        torrentHandle.AutoManaged(false)
        torrentHandle.Pause(0)
        torrentHandle.Pause()
    })
    mux.HandleFunc("/resumetorrent", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "Torrent Resumed")
        torrentHandle.AutoManaged(true)
        torrentHandle.Resume()
    })
    mux.HandleFunc("/resume", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "Torrent Started")
        session.Resume()
    })
    mux.Handle("/files/", http.StripPrefix("/files/", http.FileServer(torrentFS)))

    handler := http.Handler(mux)
    if config.idleTimeout > 0 {
        connTrackChannel := make(chan int, 10)
        handler = NewConnectionCounterHandler(connTrackChannel, mux)
        go inactiveAutoShutdown(connTrackChannel)
    }

    log.Printf("Listening HTTP on %s...\n", config.bindAddress)
    s := &http.Server{
        Addr: config.bindAddress,
        Handler: handler,
    }

    var e error
    if httpListener, e = net.Listen("tcp", config.bindAddress); e != nil {
        log.Fatal(e)
    }
    go s.Serve(httpListener)
}

func popAlert(logAlert bool) lt.Alert {
    
    alerts := session.PopAlerts()
    queueSize := alerts.Size()
    if int(queueSize) > 0 {
        for i := 0; i < int(queueSize); i++ {
            ltAlert := alerts.Get(i)
            alertPtr := ltAlert.Swigcptr()
            alertMessage := ltAlert.Message()
            alertWhat := ltAlert.What()
            if alertPtr == 0 {
                return nil
            }
            if logAlert {
                str := ""
                switch alertWhat {
                case "tracker_error_alert":
                    str = lt.SwigcptrTrackerErrorAlert(alertPtr).GetMsg()
                    break
                case "tracker_warning_alert":
                    str = lt.SwigcptrTrackerWarningAlert(alertPtr).GetMsg()
                    break
                case "scrape_failed_alert":
                    str = lt.SwigcptrScrapeFailedAlert(alertPtr).GetMsg()
                    break
                case "url_seed_alert":
                    str = lt.SwigcptrUrlSeedAlert(alertPtr).GetMsg()
                    break
                }
                if str != "" {
                    log.Printf("(%s) %s: %s", alertWhat, alertMessage, str)
                } else {
                    log.Printf("(%s) %s", alertWhat, alertMessage)
                }
            }
        }
        return alerts.Get(0)
    } else {
        return nil
    }
}

func processSaveResumeDataAlert(alert lt.Alert) {
    saveResumeDataAlert := lt.SwigcptrSaveResumeDataAlert(alert.Swigcptr())
    log.Printf("Saving resume data to: %s", config.resumeFile)
    data := lt.Bencode(saveResumeDataAlert.ResumeData())
    err := ioutil.WriteFile(config.resumeFile, []byte(data), 0644)
    if err != nil {
        log.Println(err)
    }
}

func consumeAlerts() {
    for {
        var alert lt.Alert
        if alert = popAlert(true); alert == nil {
            break
        }
        if alert.What() == "save_resume_data_alert" {
            processSaveResumeDataAlert(alert)
        }
    }
}

func buildTorrentParams(uri string) lt.AddTorrentParams {
    fileUri, err := url.Parse(uri)
    torrentParams := lt.NewAddTorrentParams()
    //defer lt.DeleteAddTorrentParams(torrentParams)
    
    error := lt.NewErrorCode()
    if err != nil {
        log.Fatal(err)
    }
    if fileUri.Scheme == "file" {
        uriPath := fileUri.Path
        if uriPath != "" && runtime.GOOS == "windows" && os.IsPathSeparator(uriPath[0]) {
            uriPath = uriPath[1:]
        }
        absPath, err := filepath.Abs(uriPath)
        if err != nil {
            log.Fatalf(err.Error())
        }
        log.Printf("Opening local file: %s", absPath)
        if _, err := os.Stat(absPath); err != nil {
            log.Fatalf(err.Error())
        }
        torrentInfo := lt.NewTorrentInfo(absPath, error)
        if error.Value() != 0 {
            log.Fatalln(error.Message())
        }
        torrentParams.SetTorrentInfo(torrentInfo)
    } else {
        log.Printf("Will fetch: %s", uri)
        torrentParams.SetUrl(uri)
    }

    log.Printf("Setting save path: %s", config.downloadPath)
    torrentParams.SetSavePath(config.downloadPath)

    if _, err := os.Stat(config.resumeFile); !os.IsNotExist(err) {
        log.Printf("loading resume file: %s", config.resumeFile)
        bytes, err := ioutil.ReadFile(config.resumeFile)
        if err != nil {
            log.Println(err)
        } else {
            resumeData := lt.NewStdVectorChar()
            defer lt.DeleteStdVectorChar(resumeData)
            for _, byte := range bytes {
                resumeData.Add(byte)
            }
            torrentParams.SetResumeData(resumeData)
        }
    }

    if config.noSparseFile {
        log.Println("Disabling sparse file support...")
        torrentParams.SetStorageMode(lt.StorageModeAllocate)
    }

    return torrentParams
}

func startServices() {
    if config.enableDHT {
        log.Println("Starting DHT...")
        PackSettings.SetStr("dht_bootstrap_nodes", strings.Join(dhtBootstrapNodes, ","))
		PackSettings.SetBool("enable_dht", true)
    }
    if config.enableLSD {
        log.Println("Starting LSD...")
        PackSettings.SetBool("enable_lsd", true)
    }
    if config.enableUPNP {
        log.Println("Starting UPNP...")
        PackSettings.SetBool("enable_upnp", true)
    }
    if config.enableNATPMP {
        log.Println("Starting NATPMP...")
        PackSettings.SetBool("enable_natpmp", true)
    }
    session.ApplySettings(PackSettings)
    for p := range mappedPorts {
		port, _ := strconv.Atoi(p)
		mappedPorts[p] = session.AddPortMapping(lt.SessionHandleTcp, port, port)
		log.Printf("Adding port mapping %v: %v", port, mappedPorts[p])
	}
    
}

func startSession() {
    log.Println("Starting session...")
    
    settings := lt.NewSettingsPack()
    
    if (config.userAgent != "") {
        settings.SetStr("user_agent", config.userAgent)
    }

	// Bools
	settings.SetBool("announce_to_all_tiers", true)
	settings.SetBool("announce_to_all_trackers", false)
	settings.SetBool("apply_ip_filter_to_trackers", false)
	settings.SetBool("lazy_bitfields", true)
	settings.SetBool("no_atime_storage", true)
	settings.SetBool("no_connect_privileged_ports", false)
	settings.SetBool("prioritize_partial_pieces", false)
	settings.SetBool("rate_limit_ip_overhead", false)
	settings.SetBool("smooth_connects", false)
	settings.SetBool("strict_end_game_mode", false)
	settings.SetBool("upnp_ignore_nonrouters", true)
	settings.SetBool("use_dht_as_fallback", false)
	settings.SetBool("use_parole_mode", true)

	// Disabling services, as they are enabled by default in libtorrent
	settings.SetBool("enable_upnp", false)
	settings.SetBool("enable_natpmp", false)
	settings.SetBool("enable_lsd", false)
	settings.SetBool("enable_dht", false)

	// settings.SetInt("peer_tos", ipToSLowCost)
	// settings.SetInt("torrent_connect_boost", 20)
	// settings.SetInt("torrent_connect_boost", 100)
	settings.SetInt("torrent_connect_boost", config.torrentConnectBoost)
	settings.SetInt("aio_threads", runtime.NumCPU()*4)
	settings.SetInt("cache_size", -1)
	settings.SetInt("mixed_mode_algorithm", int(lt.SettingsPackPreferTcp))

	// Intervals and Timeouts
	settings.SetInt("auto_scrape_interval", 1200)
	settings.SetInt("auto_scrape_min_interval", 900)
	settings.SetInt("min_announce_interval", 30)
	settings.SetInt("dht_announce_interval", 60)
	// settings.SetInt("peer_connect_timeout", 5)
	// settings.SetInt("request_timeout", 2)
	settings.SetInt("stop_tracker_timeout", 1)

	// Ratios
	settings.SetInt("seed_time_limit", 0)
	settings.SetInt("seed_time_ratio_limit", 0)
	settings.SetInt("share_ratio_limit", 0)

	// Algorithms
	settings.SetInt("choking_algorithm", int(lt.SettingsPackFixedSlotsChoker))
	settings.SetInt("seed_choking_algorithm", int(lt.SettingsPackFastestUpload))

	// Sizes
	settings.SetInt("request_queue_time", 2)
	settings.SetInt("max_out_request_queue", 5000)
	settings.SetInt("max_allowed_in_request_queue", 5000)
	// settings.SetInt("max_out_request_queue", 60000)
	// settings.SetInt("max_allowed_in_request_queue", 25000)
	// settings.SetInt("listen_queue_size", 2000)
	// settings.SetInt("unchoke_slots_limit", 20)
	settings.SetInt("max_peerlist_size", 50000)
	settings.SetInt("dht_upload_rate_limit", 50000)
	settings.SetInt("max_pex_peers", 200)
	settings.SetInt("max_suggest_pieces", 50)
	settings.SetInt("whole_pieces_threshold", 10)
	// settings.SetInt("aio_threads", 8)

	settings.SetInt("send_buffer_low_watermark", 10*1024)
	settings.SetInt("send_buffer_watermark", 500*1024)
	settings.SetInt("send_buffer_watermark_factor", 50)
    if config.maxDownloadRate >= 0 {
        settings.SetInt("download_rate_limit", config.maxDownloadRate * 1024)
    }
    if config.maxUploadRate >= 0 {
        settings.SetInt("upload_rate_limit", config.maxUploadRate * 1024)
    }

	// For Android external storage / OS-mounted NAS setups
	if config.tunedStorage && !IsMemoryStorage() {
        log.Println("Tuned Storage setup")
        settings.SetBool("use_read_cache", true)
        settings.SetBool("coalesce_reads", true)
        settings.SetBool("coalesce_writes", true)
        settings.SetInt("max_queued_disk_bytes", 12*1024*1024)
    }
    
    if (config.connectionsLimit >= 0) {
        settings.SetInt("connections_limit", config.connectionsLimit)
    }
    settings.SetInt("connection_speed", config.connectionSpeed)
    
	log.Println("Applying encryption settings...")
	settings.SetInt("allowed_enc_level", int(lt.SettingsPackPeRc4))
	settings.SetBool("prefer_rc4", true)
    
    if config.encryption != 2 {
		policy := int(lt.SettingsPackPeDisabled)
		level := int(lt.SettingsPackPeBoth)
		preferRc4 := false

		if config.encryption == 0 {
			policy = int(lt.SettingsPackPeForced)
			level = int(lt.SettingsPackPeRc4)
			preferRc4 = true
		}

		settings.SetInt("out_enc_policy", policy)
		settings.SetInt("in_enc_policy", policy)
		settings.SetInt("allowed_enc_level", level)
		settings.SetBool("prefer_rc4", preferRc4)
	}

	settings.SetInt("proxy_type", ProxyTypeNone)

	// Set alert_mask here so it also applies on reconfigure...
    settings.SetInt("alert_mask", int(
        lt.AlertStorageNotification|
        lt.AlertErrorNotification))
    
    if config.debugAlerts {
		settings.SetInt("alert_mask", int(lt.AlertAllCategories))
		settings.SetInt("alert_queue_size", 2500)
	}

	var listenPorts []string
	portLower := config.listenPort
	rand.Seed(time.Now().UTC().UnixNano())
	if config.randomPort {
        portLower = rand.Intn(16374)+49152
    }
    portUpper := portLower + 10
    for p := portLower; p <= portUpper; p++ {
		listenPorts = append(listenPorts, strconv.Itoa(p))
	}
    listenInterfaces := []string{"0.0.0.0"}
    mappedPorts = map[string]int{}
    listenInterfacesStrings := make([]string, 0)
	for _, listenInterface := range listenInterfaces {
		port := listenPorts[rand.Intn(len(listenPorts))]
		mappedPorts[port] = -1
		listenInterfacesStrings = append(listenInterfacesStrings, listenInterface+":"+port)
		if len(listenPorts) > 1 {
			port := listenPorts[rand.Intn(len(listenPorts))]
			mappedPorts[port] = -1
			listenInterfacesStrings = append(listenInterfacesStrings, listenInterface+":"+port)
		}
	}
	settings.SetStr("listen_interfaces", strings.Join(listenInterfacesStrings, ","))
	log.Printf("Listening on: %s", strings.Join(listenInterfacesStrings, ","))
    
    
    //if config.Get().LibtorrentProfile == profileMinMemory {
		//log.Info("Setting Libtorrent profile settings to MinimalMemory")
		//lt.MinMemoryUsage(settings)
	//} else if config.Get().LibtorrentProfile == profileHighSpeed {
		//log.Info("Setting Libtorrent profile settings to HighSpeed")
		//lt.HighPerformanceSeed(settings)
	//}
    
    PackSettings = settings
    
    sessionglobal = lt.NewSession(PackSettings, int(lt.SessionHandleAddDefaultPlugins))
    session = sessionglobal.GetHandle()
}

func addTorrent(torrentParams lt.AddTorrentParams) {
    log.Println("Adding torrent")
    error := lt.NewErrorCode()
    torrentHandle = session.AddTorrent(torrentParams, error)
    if error.Value() != 0 {
        log.Fatalln(error.Message())
    }

    log.Println("Enabling sequential download")
    torrentHandle.SetSequentialDownload(true)
    
    trackers := DefaultTrackers
    if config.trackers != "" {
        moretrackers := strings.Split(config.trackers, ",")
        for _, moretracker := range moretrackers {
            trackers = append(trackers, moretracker)
        }
    }
    startTier := 256-len(trackers)
    for n, tracker := range trackers {
        tracker = strings.TrimSpace(tracker)
        announceEntry := lt.NewAnnounceEntry(tracker)
        announceEntry.SetTier(byte(startTier + n))
        log.Printf("Adding tracker: %s", tracker)
        torrentHandle.AddTracker(announceEntry)
    }

    if config.enableScrape {
        log.Println("Sending scrape request to tracker")
        torrentHandle.ScrapeTracker()
    }
    
    log.Printf("Downloading torrent: %s", torrentHandle.Status().GetName())
    torrentFS = NewTorrentFS(torrentHandle, config.fileIndex, config.downloadStorage)
}

func IsMemoryStorage() bool {
	return config.downloadStorage == 1
}

func loop() {
    forceShutdown = make(chan bool, 1)
    signalChan := make(chan os.Signal, 1)
    signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
    statsTicker := time.Tick(5*time.Second)
    saveResumeDataTicker := time.Tick(30*time.Second)
    for {
        select {
        case <-forceShutdown:
            httpListener.Close()
            return
        case <-signalChan:
            forceShutdown <- true
        case <-time.After(500*time.Millisecond):
            consumeAlerts()
            torrentFS.LoadFileProgress()
            state := torrentHandle.Status().GetState()
            if config.exitOnFinish && (state == STATE_FINISHED || state == STATE_SEEDING) {
                forceShutdown <- true
            }
            if os.Getppid() == 1 {
                forceShutdown <- true
            }
        case <-statsTicker:
            stats()
        case <-saveResumeDataTicker:
            saveResumeData(true)
        }
    }
}

func main() {
    // Make sure we are properly multi-threaded, on a minimum of 2 threads
    // because we lock the main thread for lt.
    runtime.GOMAXPROCS(runtime.NumCPU())
    config.parseFlags()

    startSession()
    startServices()
    addTorrent(buildTorrentParams(config.uri))

    startHTTP()
    loop()
    shutdown()
}
