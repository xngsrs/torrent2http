torrent2http
============

It is improved torrent2http client used in [xbmctorrent](https://github.com/steeve/xbmctorrent) that has been discontinued in favor of Pulsar.
It is forked from <https://github.com/steeve/torrent2http>

If you want use it in your own KODI/XBMC plugins, use [script.module.torrent2http](https://github.com/anteo/script.module.torrent2http) add-on.
It contains pre-built version of torrent2http binaries for all platforms. You can download it from my XBMC/KODI [repository](http://bit.ly/184XKjm)


What's this
-----------

torrent2http is libtorrent-based torrent client that allows to download torrents and stream it through HTTP.


Features
--------

+ Release package contains binaries for **Android ARM,Linux x86/x64/ARM, Windows x86/x64, Darwin/OSX x64** platforms.
+ Can download torrents using __magnet://__ and __torrent__ links or downloaded __\*.torrent__ files.
+ Uses HTTP for sharing torrent info, files and peers list and for streaming content.
+ Uses **sequential** downloading mode for instant stream start.
+ Supports Content-Range, i.e. allows **seeking** through stream. This is achieved by setting deadlines for pieces that need to be loaded.
+ Enhanced **logging** letting to monitor overall progress, files progress and downloaded pieces progress.
+ Supports **fast resume** files.                                
+ Can optionally keep downloaded files if download finished.  
+ Allows to control some of libtorrent params, e.g. connections limit, rate limits, timeouts and other. 


Command line options
--------------------

      -bind="localhost:5001": Bind address of torrent2http
      -buffer=0.05: Buffer percentage from start of file
      -cmdline-proc="": Display cmdline of specified process and exit
      -connection-speed=250: The number of peer connection attempts that are made per second
      -connections-limit=50: Set a global limit on the number of connections opened
      -debug-alerts=false: Show debug alert notifications
      -dht-routers="": Additional DHT routers (comma-separated host:port pairs)
      -dl-path=".": Download path
      -dl-rate=-1: Max download rate (kB/s)
      -enable-dht=true: Enable DHT (Distributed Hash Table)
      -enable-lsd=true: Enable LSD (Local Service Discovery)
      -enable-natpmp=true: Enable NATPMP (NAT port-mapping)
      -enable-scrape=false: Enable sending scrape request to tracker (updates total peers/seeds count)
      -enable-tcp=true: Enable TCP protocol
      -enable-upnp=true: Enable UPnP (UPnP port-mapping)
      -enable-utp=true: Enable uTP protocol
      -encryption=1: Encryption: 0=forced 1=enabled (default) 2=disabled
      -exit-on-finish=false: Exit when download finished
      -file-index=-1: Start downloading file with specified index immediately (or start in paused state otherwise)
      -files-progress=false: Show files progress
      -keep-complete=false: Keep complete files after exiting
      -keep-files=false: Keep all files after exiting (incl. -keep-complete and -keep-incomplete)
      -keep-incomplete=false: Keep incomplete files after exiting
      -listen-port=6881: Use specified port for incoming connections
      -max-failcount=3: The maximum times we try to connect to a peer before stop connecting again
      -max-idle=-1: Automatically shutdown if no connection are active after a timeout
      -min-reconnect-time=60: The time to wait between peer connection attempts. If the peer fails, the time is multiplied by fail counter
      -no-sparse=false: Do not use sparse file allocation
      -overall-progress=false: Show overall progress
      -peer-connect-timeout=15: The number of seconds to wait after a connection attempt is initiated to a peer
      -pieces-progress=false: Show pieces progress
      -prioritize-partial-pieces=false: Prioritize partial pieces vs rare pieces
      -random-port=false: Use random listen port (49152-65535)
      -request-timeout=60: The number of seconds until the current front piece request will time out
      -resume-file="": Use fast resume file
      -show-stats=false: Show all stats (incl. -overall-progress -files-progress -pieces-progress)
      -state-file="": Use file for saving/restoring session state
      -strict-end-game-mode=false: "Download same block from multiple peers if one is slow"
      -torrent-connect-boost=50: The number of peers to try to connect to immediately when the first tracker response is received for a torrent
      -trackers="": Additional trackers (comma-separated URLs)
      -tuned-storage=false: Enable storage optimizations for Android external storage / OS-mounted NAS setups
      -ul-rate=-1: Max upload rate (kB/s)
      -uri="": Magnet URI or .torrent file URL
      -user-agent="torrent2http/1.0.1 libtorrent/1.0.3.0": Set an user agent


Usage
-----

Let's start torrent2http to download some torrent:

    C:\Temp>torrent2http.exe -uri="http://.../F2C2DCCDEE3822F089E8AAF04118469FBE82CF5A.torrent" -dl-path="C:\Temp" -file-index=0 -show-stats
    
    2015/01/22 14:41:38 Starting session...
    2015/01/22 14:41:38 Setting encryption settings
    2015/01/22 14:41:38 Starting DHT...
    2015/01/22 14:41:38 Starting LSD...
    2015/01/22 14:41:38 Starting UPNP...
    2015/01/22 14:41:38 Starting NATPMP...
    2015/01/22 14:41:38 Will fetch: http://..../F2C2DCCDEE3822F089E8AAF04118469FBE82CF5A.torrent
    2015/01/22 14:41:38 Setting save path: C:\Temp
    2015/01/22 14:41:38 Adding torrent
    2015/01/22 14:41:38 Enabling sequential download
    2015/01/22 14:41:38 Downloading torrent: http://..../F2C2DCCDEE3822F089E8AAF04118469FBE82CF5A.torrent
    2015/01/22 14:41:38 Starting HTTP Server...
    2015/01/22 14:41:38 Listening HTTP on localhost:5001...
    2015/01/22 14:41:39 Setting My Neighbor Totoro.avi priority to 1
    2015/01/22 14:41:39 (listen_succeeded_alert) successfully listening on [TCP] 0.0.0.0:6881
    2015/01/22 14:41:39 (listen_succeeded_alert) successfully listening on [TCP/SSL] 0.0.0.0:4433
    2015/01/22 14:41:39 (listen_succeeded_alert) successfully listening on [TCP] [::]:6881
    2015/01/22 14:41:39 (listen_succeeded_alert) successfully listening on [TCP/SSL] [::]:4433
    2015/01/22 14:41:39 (listen_succeeded_alert) successfully listening on [UDP] 0.0.0.0:6881
    2015/01/22 14:41:39 (state_changed_alert) My Neighbor Totoro.avi: state changed to: dl metadata
    2015/01/22 14:41:39 (torrent_added_alert) My Neighbor Totoro.avi added
    2015/01/22 14:41:39 (add_torrent_alert) added torrent: http://.../F2C2DCCDEE3822F089E8AAF04118469FBE82CF5A.torrent
    2015/01/22 14:41:39 (torrent_resumed_alert) My Neighbor Totoro.avi resumed
    2015/01/22 14:41:39 (metadata_received_alert) My Neighbor Totoro.avi metadata successfully received
    2015/01/22 14:41:39 (state_changed_alert) My Neighbor Totoro.avi: state changed to: downloading
    2015/01/22 14:41:39 (state_changed_alert) My Neighbor Totoro.avi: state changed to: checking (r)
    2015/01/22 14:41:39 (state_changed_alert) My Neighbor Totoro.avi: state changed to: downloading
    2015/01/22 14:41:39 (torrent_checked_alert) My Neighbor Totoro.avi checked
    2015/01/22 14:41:39 (tracker_announce_alert) My Neighbor Totoro.avi (udp://...:80/announce) sending announce (started)
    2015/01/22 14:41:39 (tracker_reply_alert) My Neighbor Totoro.avi (udp://...:80/announce) received peers: 182
    2015/01/22 14:41:43 (dht_reply_alert) My Neighbor Totoro.avi () received DHT peers: 1
    2015/01/22 14:41:43 (dht_reply_alert) My Neighbor Totoro.avi () received DHT peers: 2
    2015/01/22 14:41:44 Downloading, overall progress: 0.00%, dl/ul: 52.351/8.843 kbps, peers/seeds: 47/41, DHT nodes: 189
    2015/01/22 14:41:44 Files: [0] 0.00%
    2015/01/22 14:41:54 Downloading, overall progress: 0.35%, dl/ul: 1096.461/39.321 kbps, peers/seeds: 68/65, DHT nodes: 213
    2015/01/22 14:41:59 Files: [0] 0.16%
    2015/01/22 14:42:04 Downloading, overall progress: 1.27%, dl/ul: 1466.042/51.495 kbps, peers/seeds: 82/77, DHT nodes: 231
    ...
    
It will run until you press Ctrl+C or send /shutdown command (via HTTP request) as shown later.
As log states, torrent2http has started HTTP server on **localhost:5001** (as specified in --bind option).
Now you can request files and torrent info.


HTTP commands
-------------

You can use browser to test commands manually. Just type `http://localhost:5001/command`

### /status ###

Dumps torrent status in JSON format:

    {"name":"My Neighbor Totoro.avi","state":3,"state_str":"downloading","error":"","progress":0,"download_rate":0.01171875,
    "upload_rate":0.02734375,"total_download":0,"total_upload":68,"num_peers":0,"num_seeds":0,"total_seeds":-1,"total_peers":-1}

* Name of downloaded torrent
* State, integer from 0 to 7
* State description, one of "queued_for_checking", "checking_files", "downloading_metadata", "downloading", "finished", "seeding", "allocating", "checking_resume_data"
* Torrent error, if any
* Download progress, float in range from 0 to 1
* Download rate, kB/s
* Upload rate, kB/s
* Total downloaded bytes
* Total uploaded bytes
* Connected peers count
* Connected seeds count
* Total seeds count
* Total peers count

### /files ###

Lists all files in torrent in HTML format:

    My Neighbor Totoro.avi

### /files/\<name\> ###

Downloads/starts streaming file with specified name 

### /get/\<number\> ###

Downloads/starts streaming file with specified number

### /ls ###

Lists all files in torrent in JSON format:

    {"files":[{"name":"My Neighbor Totoro.avi","save_path":"C:\\Temp\\My Neighbor Totoro.avi",
    "url":"http://localhost:5001/files/My%20Neighbor%20Totoro.avi","size":1275165906,"offset":0,"download":44040192,
    "progress":0.03453683}]}
    
Each file information contains:

* Name of the file
* Path to the file on the disk
* URL that may be used for download or stream the file
* Size of the file
* Offset of this file in the torrent
* Downloaded bytes
* Download progress, float in range from 0 to 1

### /peers ###

Lists connected peers:

    {"peers":[{"ip":"*.*.*.*","flags":140339,"source":1,"up_speed":1.25,"down_speed":37.38086,"total_upload":0,
    "total_download":357855,"country":"","client":"μTorrent 3.4.2"},
    {"ip":"*.*.*.*","flags":140339,"source":1,"up_speed":0.13964844,"down_speed":1.0400391,"total_upload":0,
    "total_download":9945,"country":"","client":"μTorrent 3.4.2"}]}

### /trackers ###

Lists trackers:

    {"trackers":[{"url":"http://retracker.local/announce","next_announce_in":16,"min_announce_in":6,"error_code":0,
    "error_message":"","message":"","tier":0,"fail_limit":0,"fails":0,"source":0,
    "verified":false,"updating":true,"start_sent":false,"complete_sent":false},
    {"url":"http://********/announce","next_announce_in":16,
    "min_announce_in":6,"error_code":0, "error_message":"","message":"","tier":0,
    "fail_limit":0,"fails":0,"source":0,"verified":false,"updating":true,"start_sent":false,"complete_sent":false}]}

### /shutdown ###

Gracefully shuts down torrent2http. Downloaded files will be removed unless one of `--keep-files`, `--keep-complete-files` or `--keep-incomplete-files` is set


Thanks
------

To [Steeve Morin](https://github.com/steeve) for his work.
