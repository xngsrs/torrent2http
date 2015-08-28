package main

import (
	"encoding/json"
	"io/ioutil"
	"flag"
	"fmt"
	"log"
	"math/rand"
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

	lt "github.com/anteo/libtorrent-go"
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
}

type LsInfo struct {
	Files          []FileStatusInfo `json:"files"`
}

type PeerInfo struct {
	Ip             string  `json:"ip"`
	Flags          uint    `json:"flags"`
	Source         int     `json:"source"`
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
	Name           string  `json:"name"`
	State          int     `json:"state"`
	StateStr       string  `json:"state_str"`
	Error          string  `json:"error"`
	Progress       float32 `json:"progress"`
	DownloadRate   float32 `json:"download_rate"`
	UploadRate     float32 `json:"upload_rate"`
	TotalDownload  int64   `json:"total_download"`
	TotalUpload    int64   `json:"total_upload"`
	NumPeers       int     `json:"num_peers"`
	NumSeeds       int     `json:"num_seeds"`
	TotalSeeds     int     `json:"total_seeds"`
	TotalPeers     int     `json:"total_peers"`
}

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
}

const VERSION = "1.0.3"
const USER_AGENT = "torrent2http/"+VERSION+" libtorrent/"+lt.LIBTORRENT_VERSION

var (
	config Config
	session lt.Session
	torrentHandle lt.TorrentHandle
	torrentFS *TorrentFS
	forceShutdown chan bool
	httpListener net.Listener
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
	if torrentHandle == nil {
		status = SessionStatus{State: -1}
	} else {
		tstatus := torrentHandle.Status()
		status = SessionStatus{
			Name:          tstatus.GetName(),
			State:         int(tstatus.GetState()),
			StateStr:	   stateStrings[int(tstatus.GetState())],
			Error:         tstatus.GetError(),
			Progress:      tstatus.GetProgress(),
			TotalDownload: tstatus.GetTotalDownload(),
			TotalUpload:   tstatus.GetTotalUpload(),
			DownloadRate:  float32(tstatus.GetDownloadRate()) / 1024,
			UploadRate:    float32(tstatus.GetUploadRate()) / 1024,
			NumPeers:      tstatus.GetNumPeers(),
			TotalPeers:    tstatus.GetNumIncomplete(),
			NumSeeds:      tstatus.GetNumSeeds(),
			TotalSeeds:    tstatus.GetNumComplete()}
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
		for _, file := range torrentFS.Files() {
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
			Ip:              peer.Ip(),
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
			ErrorMessage:		convertToUtf8(entry.GetLastError().Message()),
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
			flag = int(lt.SessionDeleteFiles)
		} else {
			files = filesToRemove()
		}
	}
	log.Println("Removing the torrent")
	session.RemoveTorrent(torrentHandle, flag)
	if flag != 0 || len(files) > 0 {
		log.Println("Waiting for files to be removed")
		waitForAlert("cache_flushed_alert", 15*time.Second)
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
	torrentFS.Shutdown()
	if session != nil {
		session.Pause()
		waitForAlert("torrent_paused_alert", 10*time.Second)
		if torrentHandle != nil {
			saveResumeData(false)
			saveSessionState()
			removeTorrent()
		}
		log.Println("Aborting the session")
		lt.DeleteSession(session)
	}
	log.Println("Bye bye")
	os.Exit(0)
}

func parseFlags() {
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
	flag.Parse()

	if config.uri == "" {
		flag.Usage()
		os.Exit(1)
	}
	if config.resumeFile != "" && !config.keepFiles {
		fmt.Println("Usage of option -resume-file is allowed only along with -keep-files")
		os.Exit(1)
	}
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

func startHTTP() {
	log.Println("Starting HTTP Server...")

	mux := http.NewServeMux()
	mux.HandleFunc("/status", statusHandler)
	mux.HandleFunc("/ls", lsHandler)
	mux.HandleFunc("/peers", peersHandler)
	mux.HandleFunc("/trackers", trackersHandler)
	mux.Handle("/get/", http.StripPrefix("/get/", getHandler(http.FileServer(torrentFS))))
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "OK")
		forceShutdown <- true
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
	alert := session.PopAlert()
	if alert.Swigcptr() == 0 {
		return nil
	}
	if logAlert {
		str := ""
		switch alert.What() {
		case "tracker_error_alert":
			str = lt.SwigcptrTrackerErrorAlert(alert.Swigcptr()).GetMsg()
			break
		case "tracker_warning_alert":
			str = lt.SwigcptrTrackerWarningAlert(alert.Swigcptr()).GetMsg()
			break
		case "scrape_failed_alert":
			str = lt.SwigcptrScrapeFailedAlert(alert.Swigcptr()).GetMsg()
			break
		case "url_seed_alert":
			str = lt.SwigcptrUrlSeedAlert(alert.Swigcptr()).GetMsg()
			break
		}
		if str != "" {
			log.Printf("(%s) %s: %s", alert.What(), alert.Message(), str)
		} else {
			log.Printf("(%s) %s", alert.What(), alert.Message())
		}
	}
	return alert
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
		log.Printf("Loading resume file: %s", config.resumeFile)
		bytes, err := ioutil.ReadFile(config.resumeFile)
		if err != nil {
			log.Println(err)
		} else {
			resumeData := lt.NewStdVectorChar()
			count := 0
			for _, byte := range bytes {
				resumeData.PushBack(byte)
				count++
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
		session.StartDht()
	}
	if config.enableLSD {
		log.Println("Starting LSD...")
		session.StartLsd()
	}
	if config.enableUPNP {
		log.Println("Starting UPNP...")
		session.StartUpnp()
	}
	if config.enableNATPMP {
		log.Println("Starting NATPMP...")
		session.StartNatpmp()
	}
}

func startSession() {
	log.Println("Starting session...")

	session = lt.NewSession(
		lt.NewFingerprint("LT", lt.LIBTORRENT_VERSION_MAJOR, lt.LIBTORRENT_VERSION_MINOR, 0, 0),
		int(lt.SessionAddDefaultPlugins),
	)
	alertMask := uint(lt.AlertErrorNotification) | uint(lt.AlertStorageNotification) |
			     uint(lt.AlertTrackerNotification) | uint(lt.AlertStatusNotification)
	if config.debugAlerts {
		alertMask |= uint(lt.AlertDebugNotification)
	}
	session.SetAlertMask(alertMask)

	settings := session.Settings()
	settings.SetRequestTimeout(config.requestTimeout)
	settings.SetPeerConnectTimeout(config.peerConnectTimeout)
	settings.SetAnnounceToAllTrackers(true)
	settings.SetAnnounceToAllTiers(true)
	settings.SetTorrentConnectBoost(config.torrentConnectBoost)
	settings.SetConnectionSpeed(config.connectionSpeed)
	settings.SetMinReconnectTime(config.minReconnectTime)
	settings.SetMaxFailcount(config.maxFailCount)
	settings.SetRecvSocketBufferSize(1024 * 1024)
	settings.SetSendSocketBufferSize(1024 * 1024)
	settings.SetRateLimitIpOverhead(true)
	settings.SetMinAnnounceInterval(60)
	settings.SetTrackerBackoff(0)
	session.SetSettings(settings)

	if config.stateFile != "" {
		log.Printf("Loading session state from %s", config.stateFile)
		bytes, err := ioutil.ReadFile(config.stateFile)
		if err != nil {
			log.Println(err)
		} else {
			str := string(bytes)
			entry := lt.NewLazyEntry()
			error := lt.LazyBdecode(str, entry).(lt.ErrorCode)
			if error.Value() != 0 {
				log.Println(error.Message())
			} else {
				session.LoadState(entry)
			}
		}
	}

	err := lt.NewErrorCode()
	rand.Seed(time.Now().UnixNano())
	portLower := config.listenPort
	if config.randomPort {
		portLower = rand.Intn(16374)+49152
	}
	portUpper := portLower + 10
	session.ListenOn(lt.NewStdPairIntInt(portLower, portUpper), err)
	if err.Value() != 0 {
		log.Fatalln(err.Message())
	}

	settings = session.Settings()
	if (config.userAgent != "") {
		settings.SetUserAgent(config.userAgent)
	}
	if (config.connectionsLimit >= 0) {
		settings.SetConnectionsLimit(config.connectionsLimit)
	}
	if config.maxDownloadRate >= 0 {
		settings.SetDownloadRateLimit(config.maxDownloadRate * 1024)
	}
	if config.maxUploadRate >= 0 {
		settings.SetUploadRateLimit(config.maxUploadRate * 1024)
	}
	settings.SetEnableIncomingTcp(config.enableTCP)
	settings.SetEnableOutgoingTcp(config.enableTCP)
	settings.SetEnableIncomingUtp(config.enableUTP)
	settings.SetEnableOutgoingUtp(config.enableUTP)
	session.SetSettings(settings)

	if config.dhtRouters != "" {
		routers := strings.Split(config.dhtRouters, ",")
		for _, router := range routers {
			router = strings.TrimSpace(router)
			if len(router) != 0 {
				var err error
				hostPort := strings.SplitN(router, ":", 2)
				host := strings.TrimSpace(hostPort[0])
				port := 6881
				if len(hostPort) > 1 {
					port, err = strconv.Atoi(strings.TrimSpace(hostPort[1]))
					if err != nil {
						log.Fatalln(err)
					}
				}
				session.AddDhtRouter(lt.NewStdPairStringInt(host, port))
				log.Printf("Added DHT router: %s:%d", host, port)
			}
		}
	}

	log.Println("Setting encryption settings")
	encryptionSettings := lt.NewPeSettings()
	encryptionSettings.SetOutEncPolicy(byte(lt.LibtorrentPe_settingsEnc_policy(config.encryption)))
	encryptionSettings.SetInEncPolicy(byte(lt.LibtorrentPe_settingsEnc_policy(config.encryption)))
	encryptionSettings.SetAllowedEncLevel(byte(lt.PeSettingsBoth))
	encryptionSettings.SetPreferRc4(true)
	session.SetPeSettings(encryptionSettings)
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

	if config.trackers != "" {
		trackers := strings.Split(config.trackers, ",")
		startTier := 256-len(trackers)
		for n, tracker := range trackers {
			tracker = strings.TrimSpace(tracker)
			announceEntry := lt.NewAnnounceEntry(tracker)
			announceEntry.SetTier(byte(startTier + n))
			log.Printf("Adding tracker: %s", tracker)
			torrentHandle.AddTracker(announceEntry)
		}
	}

	if config.enableScrape {
		log.Println("Sending scrape request to tracker")
		torrentHandle.ScrapeTracker()
	}

	log.Printf("Downloading torrent: %s", torrentHandle.Status().GetName())
	torrentFS = NewTorrentFS(torrentHandle, config.fileIndex)
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
	parseFlags()

	startSession()
	startServices()
	addTorrent(buildTorrentParams(config.uri))

	startHTTP()
	loop()
	shutdown()
}
