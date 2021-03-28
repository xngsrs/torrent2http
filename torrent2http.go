package main

import (
    "encoding/json"
    "encoding/hex"
    "fmt"
    "io/ioutil"
    "log"
    "math"
    "math/rand"
    "net/http"
    "net/url"
    "os"
    "os/signal"
    "path"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "sync"
    "syscall"
    "time"

    lt "github.com/ElementumOrg/libtorrent-go"
)

var dhtBootstrapNodes = []string{
    "router.bittorrent.com:6881",
    "router.utorrent.com:6881",
    "dht.transmissionbt.com:6881",
    "dht.aelitis.com:6881",     // Vuze
    "dht.libtorrent.org:25401", // Libtorrent
}

var defaultTrackers = []string{
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

var srtExtensions = []string{
    ".srt",         // SubRip text file
    ".ssa", ".ass", // Advanced Substation
    ".usf", // Universal Subtitle Format
    ".idx", // VobSub
    ".sub", // MicroDVD or SubViewer
    ".aqt", // AQTitle
    ".jss", // JacoSub
    ".psb", // PowerDivX
    ".rt",  // RealText
    ".smi", // SAMI
    ".stl", // Spruce Subtitle Format
    ".pjs", // Phoenix Subtitle
}

const (
    ipToSDefault     = iota
    ipToSLowDelay    = 1 << iota
    ipToSReliability = 1 << iota
    ipToSThroughput  = 1 << iota
    ipToSLowCost     = 1 << iota
)

type FilesStatusInfo struct {
    Name     string  `json:"name"`
    SavePath string  `json:"save_path"`
    URL      string  `json:"url"`
    Size     int64   `json:"size"`
    Priority int     `json:"priority"`
    Download int64   `json:"download"`
    Progress float32 `json:"progress"`
    Offset   int64   `json:"offset"`
}

type FileStatusInfo struct {
    Name            string  `json:"name"`
    SavePath        string  `json:"save_path"`
    URL             string  `json:"url"`
    Size            int64   `json:"size"`
    Buffer          float64 `json:"bufferx"`
    Download        int64   `json:"download"`
    Progress        float32 `json:"progress"`
    State           int     `json:"state"`
    TotalDownload   int64   `json:"total_download"`
    TotalUpload     int64   `json:"total_upload"`
    DownloadRate    float32 `json:"download_rate"`
    UploadRate      float32 `json:"upload_rate"`
    NumPeers        int     `json:"num_peers"`
    NumSeeds        int     `json:"num_seeds"`
    TotalSeeds      int     `json:"total_seeds"`
    TotalPeers      int     `json:"total_peers"`
}

type LsInfo struct {
    Files []FilesStatusInfo `json:"files"`
}

type FileInfo struct {
    File []FileStatusInfo `json:"file"`
}

type PeerInfo struct {
    Ip             string  `json:"ip"`
    Flags          uint    `json:"flags"`
    Source         uint    `json:"source"`
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
    Name          string  `json:"name"`
    State         int     `json:"state"`
    StateStr      string  `json:"state_str"`
    Error         string  `json:"error"`
    Progress      float32 `json:"progress"`
    DownloadRate  float32 `json:"download_rate"`
    UploadRate    float32 `json:"upload_rate"`
    TotalDownload int64   `json:"total_download"`
    TotalUpload   int64   `json:"total_upload"`
    NumPeers      int     `json:"num_peers"`
    NumSeeds      int     `json:"num_seeds"`
    TotalSeeds    int     `json:"total_seeds"`
    TotalPeers    int     `json:"total_peers"`
    HashString    string  `json:"hash_string"`
    SessionStat   string  `json:"session_status"`
}

const (
    startBufferPercent = 0.005
    endBufferSize      = 10 * 1024 * 1024 // 10m
    minCandidateSize   = 80 * 1024 * 1024
    defaultDHTPort     = 6881
)

var (
    config                   Config
    packSettings             lt.SettingsPack
    session                  lt.SessionHandle
    sessionglobal            lt.Session
    torrentHandle            lt.TorrentHandle
    torrentInfo              lt.TorrentInfo
    torrentFS                *TorrentFS
    forceShutdown            chan bool
    fileEntryIdx             int
    lastEntryIdx             int
    bufferPiecesProgressLock sync.RWMutex
    bufferPiecesProgress     map[int]float64
    mappedPorts              map[string]int
    candidateFiles           map[int]bool
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

var stateStrings = map[int]string{
    STATE_QUEUED_FOR_CHECKING:  "queued_for_checking",
    STATE_CHECKING_FILES:       "checking_files",
    STATE_DOWNLOADING_METADATA: "downloading_metadata",
    STATE_DOWNLOADING:          "downloading",
    STATE_FINISHED:             "finished",
    STATE_SEEDING:              "seeding",
    STATE_ALLOCATING:           "allocating",
    STATE_CHECKING_RESUME_DATA: "checking_resume_data",
}

const (
    ERROR_NO_ERROR = iota
    ERROR_EXPECTED_DIGID
    ERROR_EXPECTED_COLON
    ERROR_UNEXPECTED_EOF
    ERROR_EXPECTED_VALUE
    ERROR_DEPTH_EXCEEDED
    ERROR_LIMIT_EXCEEDED
    ERROR_OVERFLOW
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

var errorStrings = map[int]string{
    ERROR_NO_ERROR:       "",
    ERROR_EXPECTED_DIGID: "expected digit in bencoded string",
    ERROR_EXPECTED_COLON: "expected colon in bencoded string",
    ERROR_UNEXPECTED_EOF: "unexpected end of file in bencoded string",
    ERROR_EXPECTED_VALUE: "expected value (list, dict, int or string) in bencoded string",
    ERROR_DEPTH_EXCEEDED: "bencoded recursion depth limit exceeded",
    ERROR_LIMIT_EXCEEDED: "bencoded item count limit exceeded",
    ERROR_OVERFLOW:       "integer overflow",
}

var forceshutdelete = false
func statusHandler(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    var status SessionStatus
    var statsesion string
    if torrentHandle == nil {
        status = SessionStatus{State: -1}
    } else {
        tstatus := torrentHandle.Status()
        defer lt.DeleteTorrentStatus(tstatus)
        if session.IsPaused() {
            statsesion = "paused"
        } else {
            statsesion = "running"
        }
        seedsTotal := tstatus.GetNumComplete()
        if seedsTotal <= 0 {
            seedsTotal = tstatus.GetListSeeds()
        }
        peersTotal := tstatus.GetNumComplete() + tstatus.GetNumIncomplete()
        if peersTotal <= 0 {
            peersTotal = tstatus.GetListPeers()
        }
        peers := tstatus.GetNumPeers() - tstatus.GetNumSeeds() 
        status = SessionStatus{
            Name:          tstatus.GetName(),
            State:         int(tstatus.GetState()),
            StateStr:      stateStrings[int(tstatus.GetState())],
            Error:         errorStrings[tstatus.GetErrc().Value()],
            Progress:      tstatus.GetProgress(),
            TotalDownload: tstatus.GetTotalDownload(),
            TotalUpload:   tstatus.GetTotalUpload(),
            DownloadRate:  float32(tstatus.GetDownloadPayloadRate()) / 1024,
            UploadRate:    float32(tstatus.GetUploadPayloadRate()) / 1024,
            NumPeers:      peers,
            TotalPeers:    peersTotal,
            NumSeeds:      tstatus.GetNumSeeds(),
            TotalSeeds:    seedsTotal,
            HashString:    hex.EncodeToString([]byte(tstatus.GetInfoHash().ToString())),
            SessionStat:   statsesion}
    }

    output, _ := json.Marshal(status)
    w.Write(output)
}

func stats() {
    status := torrentHandle.Status()
    defer lt.DeleteTorrentStatus(status)
    if !status.GetHasMetadata() {
        return
    }
    if config.showAllStats || config.showOverallProgress {
        log.Printf("%s, overall progress: %.2f%%, dl/ul: %.3f/%.3f kbps, peers/seeds: %d/%d",
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
        numFiles := torrentInfo.NumFiles()
        files := torrentInfo.Files()
        progresses := lt.NewStdVectorSizeType()
        defer lt.DeleteStdVectorSizeType(progresses)
        torrentHandle.FileProgress(progresses, int(lt.WrappedTorrentHandlePieceGranularity))
        for i := 0; i < numFiles; i++ {
            download := progresses.Get(i)
            progress := float32(download)/float32(files.FileSize(i))
            str += fmt.Sprintf("[%d] %.2f%% ", i, progress*100)
        }
        log.Println(str)
    }
}

func getHandler(h http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        index, err := strconv.Atoi(r.URL.String())
        if err == nil && torrentInfo != nil {
            for i := 0; i < torrentInfo.NumFiles(); i++ {
                if i == index {
                    torrentHandle.FilePriority(i, 1)
                } else {
                    torrentHandle.FilePriority(i, 0)
                }
            }
        }
        http.NotFound(w, r)
    })
}

func lsHandler(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    retFiles := LsInfo{}

    if torrentHandle.IsValid() && torrentInfo != nil {
        if fileEntryIdx >= 0 && fileEntryIdx < torrentInfo.NumFiles() {
            numFiles := torrentInfo.NumFiles()
            filePriorities := torrentHandle.FilePriorities()
            progresses := lt.NewStdVectorSizeType()
            defer lt.DeleteStdVectorSizeType(progresses)
            torrentHandle.FileProgress(progresses, 1)
            files := torrentInfo.Files()
            for i := 0; i < numFiles; i++ {
                prio := filePriorities.Get(i)
                download := progresses.Get(i)
                size := files.FileSize(i)
                progress := float32(download)/float32(size)
                if math.IsNaN(float64(progress)) {
                    progress = float32(0)
                }
                offset := files.FileOffset(i)
                pathname := files.FilePath(i)
                path, _ := filepath.Abs(path.Join(config.downloadPath, pathname))

                url := url.URL{
                    Host:   config.bindAddress,
                    Path:   "/files/" + pathname,
                    Scheme: "http",
                }
                fsi := FilesStatusInfo{
                    Name:     pathname,
                    Size:     size,
                    Offset:   offset,
                    Download: download,
                    Progress: progress,
                    SavePath: path,
                    URL:      url.String(),
                    Priority: prio,
                }
                retFiles.Files = append(retFiles.Files, fsi)
            }
            defer lt.DeleteStdVectorInt(filePriorities)
        }
    }

    output, _ := json.Marshal(retFiles)
    w.Write(output)
}

func fileHandler(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    retFiles := FileInfo{}

    if torrentHandle.IsValid() && torrentInfo != nil {
        if fileEntryIdx >= 0 && fileEntryIdx < torrentInfo.NumFiles() {
            status := torrentHandle.Status()
            defer lt.DeleteTorrentStatus(status)
            state := status.GetState()
            bufferProgress := float64(0)
            if state != STATE_CHECKING_FILES && state != STATE_QUEUED_FOR_CHECKING {
                bufferPiecesProgressLock.Lock()
                lenght := len(bufferPiecesProgress)
                if lenght > 0 {
                    totalProgress := float64(0)
                    piecesProgress(bufferPiecesProgress)
                    for _, v := range bufferPiecesProgress {
                        totalProgress += v
                    }
                    bufferProgress = totalProgress / float64(lenght)
                }
                bufferPiecesProgressLock.Unlock()
            }
            progresses := lt.NewStdVectorSizeType()
            defer lt.DeleteStdVectorSizeType(progresses)
            torrentHandle.FileProgress(progresses, 1)
            files := torrentInfo.Files()
            download := progresses.Get(fileEntryIdx)
            size := files.FileSize(fileEntryIdx)
            progress := float32(download)/float32(size)
            name := files.FilePath(fileEntryIdx)
            path, _ := filepath.Abs(path.Join(config.downloadPath, name))
            seedsTotal := status.GetNumComplete()
            if seedsTotal <= 0 {
                seedsTotal = status.GetListSeeds()
            }
            peersTotal := status.GetNumComplete() + status.GetNumIncomplete()
            if peersTotal <= 0 {
                peersTotal = status.GetListPeers()
            }
            peers := status.GetNumPeers() - status.GetNumSeeds() 

            url := url.URL{
                Host:   config.bindAddress,
                Path:   "/files/" + name,
                Scheme: "http",
            }
            fsi := FileStatusInfo{
                Buffer:         bufferProgress,
                Name:           name,
                Size:           size,
                SavePath:       path,
                URL:            url.String(),
                Download:       download,
                Progress:       progress,
                State:          int(state),
                TotalDownload:  status.GetTotalDownload(),
                TotalUpload:    status.GetTotalUpload(),
                DownloadRate:   float32(status.GetDownloadPayloadRate()) / 1024,
                UploadRate:     float32(status.GetUploadPayloadRate()) / 1024,
                NumPeers:       peers,
                TotalPeers:     peersTotal,
                NumSeeds:       status.GetNumSeeds(),
                TotalSeeds:     seedsTotal,
            }
            retFiles.File = append(retFiles.File, fsi)
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
            //Country:         peer.GetCountry(),
            Country:         "",
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
            ErrorMessage:		entry.GetLastError().Message().(string),
            Message:			entry.GetMessage(),
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

func prioHandler(w http.ResponseWriter, r *http.Request) {
    
    query := r.URL.Query()
    ret := ""
    index, err := strconv.Atoi(query.Get("index"))
    priority, err := strconv.Atoi(query.Get("priority"))
    if err == nil {
        if (index != fileEntryIdx) || (torrentHandle.FilePriority(index).(int) != priority){
            if priority == 9999 {
                numFiles := torrentInfo.NumFiles()
                for i := 0; i < numFiles; i++ {
                    torrentHandle.FilePriority(i, 4)
                }
                ret = "Started all files from torrent"
            } else {
                files := torrentInfo.Files()
                size := files.FileSize(index)
                lastEntryIdx = fileEntryIdx
                fileEntryIdx = index
                torrentHandle.FilePriority(index, priority)
                //torrentHandle.FilePriority(lastEntryIdx, 0)
                ret = "File named: " + files.FilePath(index) + " is set with priority " + strconv.Itoa(priority)
                if size > int64(10485760){
                    prioritizepieces()
                } else {
                    curPiece := 0
                    numPieces := torrentInfo.NumPieces()
                    startpiece, endpiece, _ := getFilePiecesAndOffset(fileEntryIdx)
                    for _ = 0; curPiece < startpiece; curPiece++ {
                        if torrentHandle.PiecePriority(curPiece).(int) > 0 {
                            torrentHandle.PiecePriority(curPiece, 1)
                            torrentHandle.SetPieceDeadline(curPiece, 1000)
                        }
                    }
                    for _ = 0; curPiece <= endpiece; curPiece++ { // get this part
                        torrentHandle.PiecePriority(curPiece, 7)
                        torrentHandle.SetPieceDeadline(curPiece, 0)
                    }
                    for _ = 0; curPiece < numPieces; curPiece++ {
                        if torrentHandle.PiecePriority(curPiece).(int) > 0 {
                            torrentHandle.PiecePriority(curPiece, 1)
                            torrentHandle.SetPieceDeadline(curPiece, 1000)
                        }
                    }
                }
            }
        }
    }
    
    w.WriteHeader(200)
    w.Write([]byte(ret))
}

func comandHandler(w http.ResponseWriter, r *http.Request) {
    
    settings := packSettings
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

func filesToRemove() []string {
    var filesToRemove []string
    if torrentInfo != nil {
        progresses := lt.NewStdVectorSizeType()
        defer lt.DeleteStdVectorSizeType(progresses)
        
        torrentHandle.FileProgress(progresses, int(lt.WrappedTorrentHandlePieceGranularity))
        numFiles := torrentInfo.NumFiles()
        for i := 0; i < numFiles; i++ {
            files := torrentInfo.Files()
            downloaded := progresses.Get(i)
            size := files.FileSize(i)
            completed := downloaded == size

            if ((!config.keepComplete || !completed) && (!config.keepIncomplete || completed)) || forceshutdelete {
                savePath, _ := filepath.Abs(path.Join(config.downloadPath, files.FilePath(i)))
                if _, err := os.Stat(savePath); !os.IsNotExist(err) {
                    filesToRemove = append(filesToRemove, savePath)
                }
            }
        }
    }
    return filesToRemove
}

func trimPathSeparator(path string) string {
    last := len(path) - 1
    if last > 0 && os.IsPathSeparator(path[last]) {
        path = path[:last]
    }
    return path
}

func removeFiles(files []string) {
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
        }
    }
}

func waitForAlert(name string, timeout time.Duration) lt.Alert {
    start := time.Now()
    var retAlert lt.Alert
    for retAlert == nil {
        for retAlert == nil {
            alert := session.WaitForAlert(lt.Milliseconds(100))
            if time.Now().Sub(start) > timeout {
                return nil
            }
            if alert.Swigcptr() != 0 {
                var alerts lt.StdVectorAlerts
                alerts = session.PopAlerts()
                defer lt.DeleteStdVectorAlerts(alerts)
                queueSize := alerts.Size()
                for i := 0; i < int(queueSize); i++ {
                    alert := alerts.Get(i)
                    //log.Printf("alert debug info: %s", alert.What())
                    if alert.What() == name {
                        retAlert = alert
                    }
                    processAlert(alert)
                }
            }
        }
    }
    return retAlert
}

func removeTorrent() {
    var flag int
    var files []string

    state := torrentHandle.Status().GetState()
    if (state != STATE_CHECKING_FILES && state != STATE_QUEUED_FOR_CHECKING && !config.keepFiles) || forceshutdelete {
        if (!config.keepComplete && !config.keepIncomplete) || forceshutdelete {
            flag = int(lt.WrappedSessionHandleDeleteFiles)
        } else {
            files = filesToRemove()
        }
    }
    log.Println("removing the torrent")
    session.RemoveTorrent(torrentHandle, flag)
    if (flag != 0) || (len(files) > 0) {
        log.Println("waiting for files to be removed")
        waitForAlert("torrent_deleted_alert", 15*time.Second)
        removeFiles(files)
    }
}

func saveResumeData(async bool) bool {
    if forceshutdelete {
        return false
    }
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
    log.Printf("saving session state to: %s", config.stateFile)
    err := ioutil.WriteFile(config.stateFile, []byte(data), 0644)
    if err != nil {
        log.Println(err)
    }
}

func shutdown() {
    log.Println("stopping torrent2http...")
    if session != nil {
        session.Pause()
        waitForAlert("torrent_paused_alert", 10*time.Second)
        if torrentHandle != nil {
            saveResumeData(false)
            saveSessionState()
            removeTorrent()
        }
        log.Println("aborting the session")
        lt.DeleteSession(sessionglobal)
    }
    if forceshutdelete {
        log.Println("deleting resume file")
        path := config.resumeFile
        if _, err := os.Stat(path); !os.IsNotExist(err) {
            err := os.Remove(path)
            if err != nil {
                log.Println("error deleting resume file")
                log.Println(err)
            }
        }
    }
    log.Println("bye bye")
    os.Exit(0)
}

func connectionCounterHandler(connTrackChannel chan int, handler http.Handler) http.Handler {
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

func startHTTP() {
    log.Println("starting HTTP Server...")

    http.HandleFunc("/status", statusHandler)
    http.HandleFunc("/ls", lsHandler)
    http.HandleFunc("/lsfile", fileHandler)
    http.HandleFunc("/peers", peersHandler)
    http.HandleFunc("/trackers", trackersHandler)
    http.HandleFunc("/command", comandHandler)
    http.Handle("/get/", http.StripPrefix("/get/", getHandler(http.FileServer(torrentFS))))
    http.HandleFunc("/priority", prioHandler)
    http.HandleFunc("/stopanddelete", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "torrent stopped and files deleted")
        forceshutdelete = true
        forceShutdown <- true
    })
    http.HandleFunc("/shutdown", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "OK")
        forceShutdown <- true
    })
    http.HandleFunc("/stop", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "Torrent Stopped")
        session.Pause()
    })
    http.HandleFunc("/pausetorrent", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "Torrent Paused")
        torrentHandle.AutoManaged(false)
        torrentHandle.Pause(0)
        torrentHandle.Pause()
    })
    http.HandleFunc("/resumetorrent", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "Torrent Resumed")
        torrentHandle.AutoManaged(true)
        torrentHandle.Resume()
    })
    http.HandleFunc("/resume", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "Torrent Started")
        session.Resume()
    })
// 	http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(torrentFS)))
    http.Handle("/files/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Connection", "close")
        handler := http.StripPrefix("/files/", http.FileServer(torrentFS))
        handler.ServeHTTP(w, r)
    }))

    handler := http.Handler(http.DefaultServeMux)
    if config.idleTimeout > 0 {
        connTrackChannel := make(chan int, 10)
        handler = connectionCounterHandler(connTrackChannel, handler)
        go inactiveAutoShutdown(connTrackChannel)
    }

    log.Printf("listening HTTP on %s\n", config.bindAddress)
    if err := http.ListenAndServe(config.bindAddress, handler); err != nil {
        log.Fatal(err)
    }
}

func logAlert(alert lt.Alert) {
    str := ""
    switch alert.What() {
    case "tracker_error_alert":
        str = lt.SwigcptrTrackerErrorAlert(alert.Swigcptr()).ErrorMessage()
        break
    case "tracker_warning_alert":
        str = lt.SwigcptrTrackerWarningAlert(alert.Swigcptr()).WarningMessage()
        break
    case "scrape_failed_alert":
        str = lt.SwigcptrScrapeFailedAlert(alert.Swigcptr()).ErrorMessage()
        break
    case "url_seed_alert":
        str = lt.SwigcptrUrlSeedAlert(alert.Swigcptr()).ErrorMessage()
        break
    }
    if str != "" {
        log.Printf("(%s) %s: %s", alert.What(), alert.Message(), str)
    } else {
        log.Printf("(%s) %s", alert.What(), alert.Message())
    }
}

func processSaveResumeDataAlert(alert lt.Alert) {
    saveResumeDataAlert := lt.SwigcptrSaveResumeDataAlert(alert.Swigcptr())
    log.Printf("saving resume data to: %s", config.resumeFile)
    data := lt.Bencode(saveResumeDataAlert.ResumeData())
    err := ioutil.WriteFile(config.resumeFile, []byte(data), 0644)
    if err != nil {
        log.Println(err)
    }
}

func processAlert(alert lt.Alert) {
    switch alert.What() {
    case "save_resume_data_alert":
        processSaveResumeDataAlert(alert)
        break
    case "metadata_received_alert":
        onMetadataReceived()
        break
    }
}

func consumeAlerts() {
    var alerts lt.StdVectorAlerts
    alerts = session.PopAlerts()
    defer lt.DeleteStdVectorAlerts(alerts)
    queueSize := alerts.Size()
    for i := 0; i < int(queueSize); i++ {
        alert := alerts.Get(i)
        logAlert(alert)
        processAlert(alert)
    }
}

func buildTorrentParams(uri string) lt.AddTorrentParams {
    fileUri, err := url.Parse(uri)
    torrentParams := lt.NewAddTorrentParams()
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
        log.Printf("opening local file: %s", absPath)
        if _, err := os.Stat(absPath); err != nil {
            log.Fatalf(err.Error())
        }
        torrentInfo := lt.NewTorrentInfo(absPath, error)
        if error.Value() != 0 {
            log.Fatalln(error.Message())
        }
        torrentParams.SetTorrentInfo(torrentInfo)
    } else {
        log.Printf("will fetch: %s", uri)
        torrentParams.SetUrl(uri)
    }

    log.Printf("setting save path: %s", config.downloadPath)
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
        log.Println("disabling sparse file support...")
        torrentParams.SetStorageMode(lt.StorageModeAllocate)
    }

    return torrentParams
}

func startServices() {
    if config.enableDHT {
        bootstrapNodes := ""
        if config.dhtRouters != "" {
            bootstrapNodes = config.dhtRouters + "," + strings.Join(dhtBootstrapNodes, ",")
        } else {
            bootstrapNodes = strings.Join(dhtBootstrapNodes, ",")
        }
        if bootstrapNodes != "" {
            log.Println("starting DHT...")
            packSettings.SetStr("dht_bootstrap_nodes", bootstrapNodes)
            packSettings.SetBool("enable_dht", true)
        }
    }
    if config.enableLSD {
        log.Println("starting LSD...")
        packSettings.SetBool("enable_lsd", true)
    }
    if config.enableUPNP {
        log.Println("starting UPNP...")
        packSettings.SetBool("enable_upnp", true)
    }
    if config.enableNATPMP {
        log.Println("starting NATPMP...")
        packSettings.SetBool("enable_natpmp", true)
    }

    session.ApplySettings(packSettings)
    for p := range mappedPorts {
        port, _ := strconv.Atoi(p)
        mappedPorts[p] = session.AddPortMapping(lt.WrappedSessionHandleTcp, port, port)
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
    settings.SetBool("announce_to_all_trackers", true)
    settings.SetBool("apply_ip_filter_to_trackers", false)
    settings.SetBool("lazy_bitfields", true)
    settings.SetBool("no_atime_storage", true)
    settings.SetBool("no_connect_privileged_ports", false)
    settings.SetBool("prioritize_partial_pieces", config.prioritizePartialPieces)
    settings.SetBool("rate_limit_ip_overhead", false)
    settings.SetBool("smooth_connects", false)
    settings.SetBool("strict_end_game_mode", config.strictEndGameMode)
    settings.SetBool("upnp_ignore_nonrouters", true)
    settings.SetBool("use_dht_as_fallback", false)
    settings.SetBool("use_parole_mode", true)
    settings.SetBool("free_torrent_hashes", true)
    settings.SetBool("announce_double_nat", true)

    // Disabling services, as they are enabled by default in libtorrent
    settings.SetBool("enable_upnp", false)
    settings.SetBool("enable_natpmp", false)
    settings.SetBool("enable_lsd", false)
    settings.SetBool("enable_dht", false)

    //settings.SetInt("peer_tos", ipToSLowCost)
    // settings.SetInt("torrent_connect_boost", 20)
    // settings.SetInt("torrent_connect_boost", 100)
    settings.SetInt("torrent_connect_boost", config.torrentConnectBoost)
    settings.SetInt("aio_threads", 1)
    settings.SetInt("aio_max", 300)
    settings.SetInt("cache_size", 1024)
    settings.SetInt("mixed_mode_algorithm", int(lt.SettingsPackPreferTcp))

    // Intervals and Timeouts
    settings.SetInt("auto_scrape_interval", 1200)
    settings.SetInt("auto_scrape_min_interval", 900)
    settings.SetInt("min_announce_interval", 30)
    settings.SetInt("dht_announce_interval", 60)
    settings.SetInt("peer_connect_timeout", config.peerConnectTimeout)
    settings.SetInt("request_timeout", config.requestTimeout)
    settings.SetInt("min_reconnect_time", config.minReconnectTime)
    settings.SetInt("stop_tracker_timeout", 1)
    settings.SetInt("max_failcount", config.maxFailCount)

    // Ratios
    settings.SetInt("seed_time_limit", 0)
    settings.SetInt("seed_time_ratio_limit", 0)
    settings.SetInt("share_ratio_limit", 0)

    // Algorithms
    settings.SetInt("choking_algorithm", int(lt.SettingsPackFixedSlotsChoker))
    //settings.SetInt("choking_algorithm", 0)
    settings.SetInt("seed_choking_algorithm", int(lt.SettingsPackFastestUpload))
    //settings.SetInt("seed_choking_algorithm", int(lt.SettingsPackRoundRobin))

    // Sizes
    settings.SetInt("request_queue_time", 2)
    settings.SetInt("max_out_request_queue", 5000)
    settings.SetInt("max_allowed_in_request_queue", 5000)
    // settings.SetInt("max_out_request_queue", 60000)
    // settings.SetInt("max_allowed_in_request_queue", 25000)
    // settings.SetInt("listen_queue_size", 2000)
    //settings.SetInt("unchoke_slots_limit", 20)
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
        settings.SetInt("choking_algorithm", int(lt.SettingsPackBittyrantChoker))
    }

    // For Android external storage / OS-mounted NAS setups
    if config.tunedStorage && !IsMemoryStorage() {
        log.Println("Tuned Storage setup")
        settings.SetBool("use_read_cache", true)
        settings.SetBool("coalesce_reads", true)
        settings.SetBool("coalesce_writes", true)
        settings.SetInt("max_queued_disk_bytes", 12 * 1024 * 1024)
    }
    
    if (config.connectionsLimit >= 0) {
        settings.SetInt("connections_limit", config.connectionsLimit)
    }
    settings.SetInt("connection_speed", config.connectionSpeed)
    
    log.Println("Applying encryption settings...")
    var policy int
    var level int
    var preferRc4 bool
    
    if config.encryption == 2 {
        policy = int(lt.SettingsPackPeDisabled)
        level = int(lt.SettingsPackPeBoth)
        preferRc4 = false
    }
    if config.encryption == 1 {
        policy = int(lt.SettingsPackPeEnabled)
        level = int(lt.SettingsPackPeBoth)
        preferRc4 = false
    }

    if config.encryption == 0 {
            policy = int(lt.SettingsPackPeForced)
            level = int(lt.SettingsPackPeRc4)
            preferRc4 = true
    }
    //log.Printf("Enc Policy: %d, allowed_enc_level: %d, prefer_rc4: %s", policy, level, preferRc4)
    settings.SetInt("out_enc_policy", policy)
    settings.SetInt("in_enc_policy", policy)
    settings.SetInt("allowed_enc_level", level)
    settings.SetBool("prefer_rc4", preferRc4)

    settings.SetInt("proxy_type", ProxyTypeNone)

    // Set alert_mask here so it also applies on reconfigure...
    settings.SetInt("alert_mask", int(lt.AlertErrorNotification) | int(lt.AlertStorageNotification) |
        int(lt.AlertTrackerNotification) | int(lt.AlertStatusNotification))
    
    if config.debugAlerts {
        settings.SetInt("alert_mask", int(lt.AlertAllCategories))
        settings.SetInt("alert_queue_size", 2500)
    }
    
    var listenPorts []string
    rand.Seed(time.Now().UTC().UnixNano())
    portLower := config.listenPort
    if config.randomPort {
        portLower = rand.Intn(16374)+49152
    }
    portUpper := portLower + 5

    for p := portLower; p <= portUpper; p++ {
        listenPorts = append(listenPorts, strconv.Itoa(p))
    }

    listenInterfaces := []string{"0.0.0.0"}
    rand.Seed(time.Now().UTC().UnixNano())
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
// 	var listenPorts []string
// 	portLower := config.listenPort
// 	rand.Seed(time.Now().UTC().UnixNano())
// 	if config.randomPort {
//         portLower = rand.Intn(16374)+49152
//     }
//     portUpper := portLower + 4
//     for p := portLower; p <= portUpper; p++ {
// 		listenPorts = append(listenPorts, strconv.Itoa(p))
// 	}
//     listenInterfaces := []string{"0.0.0.0"}
//     mappedPorts = map[string]int{}
//     listenInterfacesStrings := make([]string, 0)
// 	for _, listenInterface := range listenInterfaces {
//         for i := range listenPorts {
//             port := listenPorts[i]
//             mappedPorts[port] = -1
//             listenInterfacesStrings = append(listenInterfacesStrings, listenInterface+":"+port)
//         }
// 	}
// 	settings.SetStr("listen_interfaces", strings.Join(listenInterfacesStrings, ","))
// 	log.Printf("Listening on: %s", strings.Join(listenInterfacesStrings, ","))
    
    
    //if config.LibtorrentProfile == profileMinMemory {
        //log.Info("Setting Libtorrent profile settings to MinimalMemory")
        //lt.MinMemoryUsage(settings)
    //} else if config.LibtorrentProfile == profileHighSpeed {
        //log.Info("Setting Libtorrent profile settings to HighSpeed")
        //lt.HighPerformanceSeed(settings)
    //}
    var err error
    packSettings = settings
    
    sessionglobal, err = lt.NewSession(packSettings, int(lt.WrappedSessionHandleAddDefaultPlugins))
    if err != nil {
        log.Printf("Could not create libtorrent session: %s", err)
        return
    }
    session, err = sessionglobal.GetHandle()
    if err != nil {
        log.Printf("Could not create libtorrent session handle: %s", err)
        return
    }
}

func chooseFile() int {
    biggestFileIndex := int(-1)
    maxSize := int64(0)
    numFiles := torrentInfo.NumFiles()
    candidateFiles = make(map[int]bool)
    files := torrentInfo.Files()

    for i := 0; i < numFiles; i++ {
        size := files.FileSize(i)
        if size > maxSize {
            maxSize = size
            biggestFileIndex = i
        }
        if size > minCandidateSize {
            candidateFiles[i] = true
        }
    }

    log.Printf("there are %d candidate file(s)", len(candidateFiles))

    if config.fileIndex >= 0 {
        if _, ok := candidateFiles[config.fileIndex]; ok {
            log.Printf("selecting requested file at position %d", config.fileIndex)
            return config.fileIndex
        }
        log.Print("unable to select requested file")
    }

    log.Printf("selecting biggest file (position:%d size:%dkB)", biggestFileIndex, maxSize/1024)
    return biggestFileIndex
}

func pieceFromOffset(offset int64) (int, int64) {
    pieceLength := int64(torrentInfo.PieceLength())
    piece := int(offset / pieceLength)
    pieceOffset := offset % pieceLength
    return piece, pieceOffset
}

func IsSubtitlesExt(ext string) bool {
    for _, e := range srtExtensions {
        if ext == e {
            return true
        }
    }
    return false
}

func getFilePiecesAndOffset(ind int) (int, int, int64) {
    files := torrentInfo.Files()
    startPiece, offset := pieceFromOffset(files.FileOffset(ind))
    endPiece, _ := pieceFromOffset(files.FileOffset(ind) + files.FileSize(ind))
    return startPiece, endPiece, offset
}

func addTorrent(torrentParams lt.AddTorrentParams) {
    log.Println("adding torrent")
    var err error
    torrentHandle, err = session.AddTorrent(torrentParams)
    if err != nil {
        log.Printf("Error adding torrent: %s", err)
    }

    log.Println("enabling sequential download")
    torrentHandle.SetSequentialDownload(true)

    //trackers := defaultTrackers
    var trackers []string
    if config.trackers != "" {
        trackers = strings.Split(config.trackers, ",")
    }
    startTier := 256 - len(trackers)
    for n, tracker := range trackers {
        tracker = strings.TrimSpace(tracker)
        announceEntry := lt.NewAnnounceEntry(tracker)
        announceEntry.SetTier(byte(startTier + n))
        log.Printf("adding tracker: %s", tracker)
        torrentHandle.AddTracker(announceEntry)
    }

    if config.enableScrape {
        log.Println("sending scrape request to tracker")
        torrentHandle.ScrapeTracker()
    }

    log.Printf("downloading torrent: %s", torrentHandle.Status().GetName())
    torrentFS = NewTorrentFS(torrentHandle, config.downloadPath)

    if torrentHandle.Status().GetHasMetadata() {
        onMetadataReceived()
    }
}

func onMetadataReceived() {
    log.Printf("metadata received")

    torrentInfo = torrentHandle.TorrentFile()
    
    fileEntryIdx = chooseFile()

    numFiles := torrentInfo.NumFiles()
    filepriorities := torrentHandle.FilePriorities()
    defer lt.DeleteStdVectorInt(filepriorities)
    
    if config.fileIndex == 9999 || config.fileIndex == -1 {
        for i := 0; i < numFiles; i++ {
            if config.fileIndex == 9999 {
                filepriorities.Set(i, 4)
            } else {
                filepriorities.Set(i, 0)
            }
        }
    } else {
        for i := 0; i < numFiles; i++ {
            if i == fileEntryIdx{
                filepriorities.Set(i, 7)
            } else {
                filepriorities.Set(i, 0)
            }
        }
    }
    torrentHandle.PrioritizeFiles(filepriorities)
    if config.fileIndex == 9999 || config.fileIndex == -1 {
        log.Printf("Not prioritizing pieces this time")
    } else {
        prioritizepieces()
    }
}

func prioritizepieces() {
    log.Print("setting piece priorities")
    files := torrentInfo.Files()
    offsetdoi := files.FileOffset(fileEntryIdx)
    size := files.FileSize(fileEntryIdx)
    pieceLength := int64(torrentInfo.PieceLength())
    startPiece := int(offsetdoi / pieceLength)
    endPiece := int((offsetdoi + size) / pieceLength)
    startLength := float64(endPiece-startPiece) * float64(pieceLength) * config.buffer
    startBufferPieces := int(math.Ceil(startLength / float64(pieceLength)))
    // Prefer a fixed size, since metadata are very rarely over endPiecesSize=10MB anyway.
    endBufferPieces := int(math.Ceil(float64(endBufferSize) / float64(pieceLength)))

    piecesPriorities := lt.NewStdVectorInt()
    defer lt.DeleteStdVectorInt(piecesPriorities)

    bufferPiecesProgress = make(map[int]float64)
    bufferPiecesProgressLock.Lock()
    defer bufferPiecesProgressLock.Unlock()
    
    if lastEntryIdx >= 0 {
        torrentHandle.ClearPieceDeadlines()
    }

    // Properly set the pieces priority vector
    curPiece := 0
    for _ = 0; curPiece < startPiece; curPiece++ {
        piecesPriorities.Add(0)
    }
    for _ = 0; curPiece <= startPiece+startBufferPieces; curPiece++ { // get this part
        piecesPriorities.Add(7)
        bufferPiecesProgress[curPiece] = 0
        torrentHandle.SetPieceDeadline(curPiece, 0)
    }
    for _ = 0; curPiece < endPiece-endBufferPieces; curPiece++ {
        piecesPriorities.Add(1)
//         torrentHandle.SetPieceDeadline(curPiece, 500)
    }
    for _ = 0; curPiece <= endPiece; curPiece++ { // get this part
        piecesPriorities.Add(7)
        bufferPiecesProgress[curPiece] = 0
        torrentHandle.SetPieceDeadline(curPiece, 0)
    }
    numPieces := torrentInfo.NumPieces()
    for _ = 0; curPiece < numPieces; curPiece++ {
        piecesPriorities.Add(0)
    }
    torrentHandle.PrioritizePieces(piecesPriorities)
//     numFiles := torrentInfo.NumFiles()
//     log.Printf("prioritizing also small files")
//     for i := 0; i < numFiles; i++ {
//         filepatH := files.FilePath(i)
//         extension := filepath.Ext(filepatH)
//         if !IsSubtitlesExt(extension){
//             startpiece, endpiece, _ := getFilePiecesAndOffset(i)
//             for j := startpiece; j <= endpiece; j++ {
//                 torrentHandle.PiecePriority(j, 7)
//             }
//         }
//     }
//     torrentHandle.ForceReannounce()
// 	if config.enableDHT {
// 		torrentHandle.ForceDhtAnnounce()
// 	}
}

func piecesProgress(pieces map[int]float64) {
    queue := lt.NewStdVectorPartialPieceInfo()
    defer lt.DeleteStdVectorPartialPieceInfo(queue)

    torrentHandle.GetDownloadQueue(queue)
    for piece := range pieces {
        if torrentHandle.HavePiece(piece) == true {
            pieces[piece] = 1.0
        }
    }
    blockSize := torrentHandle.Status().GetBlockSize()
    queueSize := queue.Size()
    for i := 0; i < int(queueSize); i++ {
        ppi := queue.Get(i)
        pieceIndex := ppi.GetPieceIndex()
        if v, exists := pieces[pieceIndex]; exists && v != 1.0{
            totalBlockDownloaded := ppi.GetFinished() * blockSize
            totalBlockSize := ppi.GetBlocksInPiece() * blockSize
            pieces[pieceIndex] = float64(totalBlockDownloaded) / float64(totalBlockSize)
        }
    }
}

func IsMemoryStorage() bool {
    return true
}

func handleSignals() {
    forceShutdown = make(chan bool, 1)
    signalChan := make(chan os.Signal, 1)
    saveResumeDataTicker := time.Tick(30 * time.Second)
    signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

    for {
        select {
        case <-forceShutdown:
            shutdown()
            return
        case <-signalChan:
            forceShutdown <- true
        case <-time.After(500 * time.Millisecond):
            consumeAlerts()
            status := torrentHandle.Status()
            defer lt.DeleteTorrentStatus(status)
            state := status.GetState()
            if config.exitOnFinish && (state == STATE_FINISHED || state == STATE_SEEDING) {
                forceShutdown <- true
            }
            if os.Getppid() == 1 {
                forceShutdown <- true
            }
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

    go handleSignals()
    startHTTP()
}
