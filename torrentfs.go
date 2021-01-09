package main

import (
    "errors"
    "log"
    "math"
    "net/http"
    "net/url"
    "io"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"
    "unsafe"

    lt "github.com/ElementumOrg/libtorrent-go"
    "github.com/anacrolix/missinggo/perf"
)

const (
    piecesRefreshDuration = 350 * time.Millisecond
)

type TorrentFS struct {
	handle   lt.TorrentHandle
	Dir      http.Dir
	readers  map[int64]*TorrentFile
	muReaders *sync.Mutex
}

type TorrentFile struct {
	http.File
	tfs               *TorrentFS
	torrentInfo       lt.TorrentInfo
	fileEntryIdx      int
	pieceLength       int
	fileOffset        int64
	fileSize          int64
	piecesMx          sync.RWMutex
	pieces            Bitfield
	piecesLastUpdated time.Time
	lastStatus        lt.TorrentStatus
	closed            bool
	savePath          string
	
	id                int64
	readahead         int64
	
	seeked            Event
	removed           Event
	
	lastUsed          time.Time
	isActive          bool
}

// PieceRange ...
type PieceRange struct {
	Begin, End int
}

func NewTorrentFS(handle lt.TorrentHandle, path string) *TorrentFS {
    tfs := TorrentFS{
        handle:   handle,
        Dir:      http.Dir(path),
        readers:  map[int64]*TorrentFile{},
        muReaders: &sync.Mutex{},
        
    }
    return &tfs
}

var (
	fileindex        int
)

func (tfs *TorrentFS) Open(uname string) (http.File, error) {
    name := DecodeFileURL(uname)
    
    var file http.File
	var err error
    
    if name == "/" {
        return nil, errors.New("file no found")
    }
    file, err = os.Open(filepath.Join(string(tfs.Dir), name))
    if err != nil {
        log.Printf("File not yet downloaded: %s", err)
        return nil, err
    }
    // make sure we don't open a file that's locked, as it can happen
    // on BSD systems (darwin included)
    if err := unlockFile(file.(*os.File)); err != nil {
        log.Printf("unable to unlock file because: %s", err)
    }

    if !tfs.handle.IsValid() {
        return nil, errors.New("file is not found")
    }

    torrentInfo := tfs.handle.TorrentFile()
    numFiles := torrentInfo.NumFiles()
    files := torrentInfo.Files()
    for j := 0; j < numFiles; j++ {
        path := files.FilePath(j)
        if name[1:] == path {
            return NewTorrentFile(file, tfs, torrentInfo, j, files.FileOffset(j), files.FileSize(j), path)
        }
    }
    return nil, errors.New("file not yet found")
    //defer lt.DeleteTorrentInfo(torrentInfo)

    //return file, err
}

// GetReadaheadSize ...
func (tfs *TorrentFS) GetReadaheadSize() (ret int64) {
	defer perf.ScopeTimer()()

	defaultRA := int64(50 * 1024 * 1024)
	return defaultRA
}

// CloseReaders ...
func (tfs *TorrentFS) CloseReaders() {
	tfs.muReaders.Lock()
	defer tfs.muReaders.Unlock()

	for k, r := range tfs.readers {
		log.Printf("Closing active reader: %d", r.id)
		r.Close()
		delete(tfs.readers, k)
	}
}

// ResetReaders ...
func (tfs *TorrentFS) ResetReaders() {
	tfs.muReaders.Lock()
	defer tfs.muReaders.Unlock()

	if len(tfs.readers) == 0 {
		return
	}

	perReaderSize := tfs.GetReadaheadSize()
	countActive := float64(0)
	countIdle := float64(0)
	for _, r := range tfs.readers {
		if r.IsActive() {
			countActive++
		} else {
			countIdle++
		}
	}

	sizeActive := int64(0)
	sizeIdle := int64(0)

	if countIdle > 1 {
		countIdle = 2
	}
	if countActive > 1 {
		countActive = 2
	}

	if countIdle > 0 {
		sizeIdle = int64(float64(perReaderSize) * 0.33)
		if countActive > 0 {
			sizeActive = perReaderSize - sizeIdle
		}
	} else if countActive > 0 {
		sizeActive = int64(float64(perReaderSize) / countActive)
	}

	if countActive == 0 && countIdle == 0 {
		return
	}

	for _, r := range tfs.readers {
		size := sizeActive
		if !r.IsActive() {
			size = sizeIdle
		}

		if r.readahead == size {
			continue
		}

		//log.Printf("Setting readahead for reader %d", r.id)
		r.readahead = size
	}
}

// ReadersReadaheadSum ...
func (tfs *TorrentFS) ReadersReadaheadSum() int64 {
	tfs.muReaders.Lock()
	defer tfs.muReaders.Unlock()

	if len(tfs.readers) == 0 {
		return 0
	}

	res := int64(0)
	for _, r := range tfs.readers {
		res += r.Readahead()
	}

	return res
}

func NewTorrentFile(file http.File, tfs *TorrentFS, torrentInfo lt.TorrentInfo, fileEntryIdx int, offset int64, size int64, path string) (*TorrentFile, error) {
	tf := &TorrentFile{
		File:         file,
		tfs:          tfs,
		torrentInfo:  torrentInfo,
		fileEntryIdx: fileEntryIdx,
		pieceLength:  torrentInfo.PieceLength(),
		fileOffset:   offset,
		fileSize:     size,
        
        id:           time.Now().UTC().UnixNano(),

        lastUsed:     time.Now(),
        isActive:     true,

	}
	if fileindex != fileEntryIdx {
        tf.log("opening file %s", path)
    }
    
    tfs.muReaders.Lock()
	tfs.readers[tf.id] = tf
	tfs.muReaders.Unlock()

	tfs.ResetReaders()
    
	return tf, nil
}

func (tf *TorrentFile) log(message string, v ...interface{}) {
    args := append([]interface{}{tf.fileEntryIdx}, v...)
    log.Printf("[%d] "+message+"\n", args...)
}

func (tf *TorrentFile) updatePieces() error {
    tf.piecesMx.Lock()
    defer tf.piecesMx.Unlock()

    if time.Now().After(tf.piecesLastUpdated.Add(piecesRefreshDuration)) {
        // need to keep a reference to the status or else the pieces bitfield
        // is at risk of being collected
        tf.GetLastStatus(true)
        //tf.lastStatus = tf.tfs.handle.Status(uint(lt.WrappedTorrentHandleQueryPieces))
        if tf.lastStatus.GetState() > lt.TorrentStatusSeeding {
            return errors.New("torrent file has invalid state")
        }
        piecesBits := tf.lastStatus.GetPieces()
        piecesBitsSize := piecesBits.Size() + 1
        piecesSliceSize := piecesBitsSize / 8
        if piecesBitsSize%8 > 0 {
            // Add +1 to round up the bitfield
            piecesSliceSize++
        }
        data := (*[100000000]byte)(unsafe.Pointer(piecesBits.Bytes()))[:piecesSliceSize]
        tf.pieces = Bitfield(data)
        tf.piecesLastUpdated = time.Now()
    }
    return nil
}

func (tf *TorrentFile) getPieces() (int, int) {
    startPiece, _ := tf.pieceFromOffset(1)
    endPiece, _ := tf.pieceFromOffset(tf.fileSize - 1)
    return startPiece, endPiece
}

func (tf *TorrentFile) pieceFromOffset(offset int64) (int, int) {
    piece := (tf.fileOffset + offset) / int64(tf.pieceLength)
    pieceOffset := (tf.fileOffset + offset) % int64(tf.pieceLength)
    return int(piece), int(pieceOffset)
}

func (tf *TorrentFile) hasPiece(idx int) bool {
    if err := tf.updatePieces(); err != nil {
        return false
    }
    tf.piecesMx.RLock()
    defer tf.piecesMx.RUnlock()
    return tf.pieces.GetBit(idx)
}

func (tf *TorrentFile) waitForPiece(piece int) error {
    if tf.hasPiece(piece) {
        return nil
    }

    defer perf.ScopeTimer()()
    tf.log("waiting for piece %d", piece)
    tf.tfs.handle.PiecePriority(piece, 6)
    tf.tfs.handle.SetPieceDeadline(piece, 0)

    ticker := time.Tick(piecesRefreshDuration)
    removed := tf.removed.C()
    seeked := tf.seeked.C()
    
    for tf.hasPiece(piece) == false {
        select {
        case <-seeked:
			tf.seeked.Clear()

			log.Printf("Unable to wait for piece %d as file was seeked", piece)
			return errors.New("File was seeked")
        case <-removed:
			log.Printf("Unable to wait for piece %d as file was closed", piece)
			return errors.New("File was closed")
        case <-ticker:
            if tf.tfs.handle.PiecePriority(piece).(int) == 0 || tf.closed {
                return errors.New("file was closed")
            }
            continue
        }
    }
    _, endPiece := tf.getPieces()
    for i := piece+1; i < piece+4; i++ {
        if i < endPiece && !tf.hasPiece(i) {
            tf.tfs.handle.PiecePriority(i, 6)
            tf.tfs.handle.SetPieceDeadline(i, 0)
        }
    }
    return nil
}

func (tf *TorrentFile) Read(data []byte) (n int, err error) {
    defer perf.ScopeTimer()()
    tf.SetActive(true)
    
    currentOffset, err := tf.File.Seek(0, io.SeekCurrent)
    if err != nil {
        return 0, err
    }

    left := len(data)
    pos := 0
    
    piece, pieceOffset := tf.pieceFromOffset(currentOffset)
    
    for left > 0 && err == nil {
        size := left
        
        if err = tf.waitForPiece(piece); err != nil {
			log.Printf("Wait failed: %d", piece)
			continue
		}
    
        if pieceOffset+size > tf.pieceLength {
                size = tf.pieceLength - pieceOffset
            }
            
        b := data[pos : pos+size]
        n1 := 0
        
        n1, err = tf.File.Read(b)
    
    if err != nil {
			if err == io.ErrShortBuffer {
				log.Printf("Retrying to fetch piece: %d", piece)
				err = nil
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return
		} else if n1 > 0 {
			n += n1
			left -= n1
			pos += n1

			currentOffset += int64(n1)
			piece, pieceOffset = tf.pieceFromOffset(currentOffset)
		} else {
			return
		}
	}

	return
}

func (tf *TorrentFile) Seek(offset int64, whence int) (int64, error) {
    defer perf.ScopeTimer()()
    tf.SetActive(true)
    
    seekingOffset := offset

    switch whence {
    case io.SeekStart:
		for _, r := range tf.tfs.readers {
			if r.id == tf.id {
				continue
			}

			r.SetActive(false)
		}

		piece, _ := tf.pieceFromOffset(seekingOffset)

        if tf.hasPiece(piece) == false {
            tf.log("we don't have piece %d, setting piece priorities", piece)
            piecesPriorities := lt.NewStdVectorInt()
            defer lt.DeleteStdVectorInt(piecesPriorities)

            curPiece := 0
            numPieces := tf.torrentInfo.NumPieces()
            startPiece, endPiece := tf.getPieces()
            buffPieces := int(math.Ceil(float64(endPiece-startPiece) * startBufferPercent))
            if buffPieces == 0 {
                buffPieces = 1
            }
            if piece+buffPieces > endPiece {
                buffPieces = endPiece - piece
            }
            for _ = 0; curPiece < piece; curPiece++ {
                piecesPriorities.Add(0)
            }
            for _ = 0; curPiece < piece+buffPieces; curPiece++ { //highest priority for buffer
                piecesPriorities.Add(7)
                tf.tfs.handle.SetPieceDeadline(curPiece, 0, 0)
            }
            for _ = 0; curPiece <= endPiece; curPiece++ { // to the end of a file
                piecesPriorities.Add(1)
            }
            for _ = 0; curPiece < numPieces; curPiece++ {
                piecesPriorities.Add(0)
            }
            tf.tfs.handle.PrioritizePieces(piecesPriorities)
        }
		break
    case io.SeekCurrent:
        currentOffset, err := tf.File.Seek(0, io.SeekCurrent)
        if err != nil {
            return currentOffset, err
        }
        seekingOffset += currentOffset
        break
    case io.SeekEnd:
        seekingOffset = tf.fileSize - offset
        break
    }

    tf.log("seeking at %d/%d", seekingOffset, tf.fileSize)
    return tf.File.Seek(offset, whence)
}

func (tf *TorrentFile) Close() error {
    defer perf.ScopeTimer()()
    
    if tf.closed {
        return nil
    }
    files := torrentInfo.Files()
    tf.log("closing %s...", files.FilePath(fileEntryIdx))
    tf.removed.Set()
    tf.tfs.muReaders.Lock()
	delete(tf.tfs.readers, tf.id)
	tf.tfs.muReaders.Unlock()

	defer tf.tfs.ResetReaders()
    tf.closed = true
    if tf.File == nil {
        return nil
    }
    return tf.File.Close()
}

func (tf *TorrentFile) ShowPieces() {
    startPiece, endPiece := tf.getPieces()
    str := ""
    for i := startPiece; i <= endPiece; i++ {
        if tf.pieces.GetBit(i) == false {
            str += "-"
        } else {
            str += "#"
        }
    }
    tf.log(str)
}

func (tf *TorrentFile) GetLastStatus(isForced bool) lt.TorrentStatus {
	if !isForced && tf.lastStatus != nil && tf.lastStatus.Swigcptr() != 0 {
		return tf.lastStatus
	}

	if tf.lastStatus != nil && tf.lastStatus.Swigcptr() != 0 {
		lt.DeleteTorrentStatus(tf.lastStatus)
	}

	tf.lastStatus = tf.tfs.handle.Status(uint(lt.WrappedTorrentHandleQueryPieces))
	return tf.lastStatus
}

// ReaderPiecesRange ...
func (tf *TorrentFile) ReaderPiecesRange() (ret PieceRange) {
	pos, _ := tf.Pos()
	ra := tf.Readahead()

	return tf.byteRegionPieces(tf.torrentOffset(pos), ra)
}

// Readahead returns current reader readahead
func (tf *TorrentFile) Readahead() int64 {
	ra := tf.readahead
	if ra < 1 {
		// Needs to be at least 1, because [x, x) means we don't want
		// anything.
		ra = 1
	}
	pos, _ := tf.Pos()
	if tf.fileSize > 0 && ra > tf.fileSize-pos {
		ra = tf.fileSize - pos
	}
	return ra
}

// Pos returns current file position
func (tf *TorrentFile) Pos() (int64, error) {
	return tf.File.Seek(0, io.SeekCurrent)
}

func (tf *TorrentFile) torrentOffset(readerPos int64) int64 {
	return tf.fileOffset + readerPos
}

// Returns the range of pieces [begin, end) that contains the extent of bytes.
func (tf *TorrentFile) byteRegionPieces(off, size int64) (pr PieceRange) {
	if off >= tf.torrentInfo.TotalSize() {
		return
	}
	if off < 0 {
		size += off
		off = 0
	}
	if size <= 0 {
		return
	}

	pl := int64(tf.pieceLength)
	pr.Begin = NumMax(0, int(off/pl))
	pr.End = NumMin(tf.torrentInfo.NumPieces()-1, int((off+size-1)/pl))

	return
}

// IsIdle ...
func (tf *TorrentFile) IsIdle() bool {
	return tf.lastUsed.Before(time.Now().Add(time.Minute * -1))
}

// IsActive ...
func (tf *TorrentFile) IsActive() bool {
	return tf.isActive
}

// SetActive ...
func (tf *TorrentFile) SetActive(is bool) {
	if is != tf.isActive {
		defer tf.tfs.ResetReaders()
	}

	if is {
		tf.lastUsed = time.Now()
		tf.isActive = true
	} else {
		tf.isActive = false
	}
}

func NumMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Max ...
func NumMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// DecodeFileURL decodes file path from url
func DecodeFileURL(u string) (ret string) {
	us := strings.Split(u, string("/"))
	for _, v := range us {
		v, _ = url.PathUnescape(v)
	}

	return strings.Join(us, string(os.PathSeparator))
}
