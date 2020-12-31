package main

import (
    "log"
    "net/http"
    "os"
    "io"
    "path"
    "path/filepath"
    "errors"
    "time"

    lt "github.com/ElementumOrg/libtorrent-go"
)

type TorrentFS struct {
    handle            lt.TorrentHandle
    info              lt.TorrentInfo
    priorities		  map[int]int
    openedFiles	      []*TorrentFile
    lastOpenedFile    *TorrentFile
    shuttingDown	  bool
    fileCounter		  int
    progresses		  lt.StdVectorSizeType
    ms                lt.MemoryStorage
    DownloadStorage   int
}

type TorrentFile struct {
    tfs               *TorrentFS
    num				  int
    closed			  bool
    savePath          string
    fileEntry         lt.FileEntry
    index             int
    filePtr           *os.File
    downloaded        int64
    progress          float32
}

type TorrentDir struct {
    tfs         *TorrentFS
    entriesRead int
}

var (
    ErrFileNotFound = errors.New("File is not found")
    ErrInvalidIndex = errors.New("No file with such index")
)

func NewTorrentFS(handle lt.TorrentHandle, startIndex int, downloadStorage int) *TorrentFS {
    tfs := TorrentFS{
        handle: handle,
        priorities: make(map[int]int),
        DownloadStorage: downloadStorage,
    }
    go func() {
        tfs.waitForMetadata()
        
        if startIndex < 0 {
            log.Println("No -file-index specified, downloading will be paused until any file is requested")
        }
        if startIndex == 9999 {
            for i := 0; i < tfs.TorrentInfo().NumFiles(); i++ {
                tfs.setPriority(i, 4)
            }
        } else {
            for i := 0; i < tfs.TorrentInfo().NumFiles(); i++ {
                if startIndex == i {
                    tfs.setPriority(i, 4)
                } else {
                    tfs.setPriority(i, 0)
                }
            }
        }
    }()
    return &tfs
}

func (tfs *TorrentFS) Shutdown() {
    tfs.shuttingDown = true
    if len(tfs.openedFiles) > 0 {
        log.Printf("Closing %d opened file(s)", len(tfs.openedFiles))
        for _, f := range tfs.openedFiles {
            f.Close()
        }
    }
}

func (tfs *TorrentFS) LastOpenedFile() *TorrentFile {
    return tfs.lastOpenedFile
}

func (tfs *TorrentFS) addOpenedFile(file *TorrentFile) {
    tfs.openedFiles = append(tfs.openedFiles, file)
}

func (tfs *TorrentFS) setPriority(index int, priority int) {
    if val, ok := tfs.priorities[index]; !ok || val != priority {
        log.Printf("Setting %s priority to %d", tfs.info.FileAt(index).GetPath(), priority)
        tfs.priorities[index] = priority
        tfs.handle.FilePriority(index, priority)
        time.Sleep(50 * time.Millisecond)
    }
}

func (tfs *TorrentFS) findOpenedFile(file *TorrentFile) int {
    for i, f := range tfs.openedFiles {
        if f == file {
            return i
        }
    }
    return -1
}

func (tfs *TorrentFS) removeOpenedFile(file *TorrentFile) {
    pos := tfs.findOpenedFile(file)
    if pos >= 0 {
        tfs.openedFiles = append(tfs.openedFiles[:pos], tfs.openedFiles[pos+1:]...)
    }
}

func (tfs *TorrentFS) waitForMetadata() {
    for !tfs.handle.Status().GetHasMetadata() {
        time.Sleep(100 * time.Millisecond)
    }
    tfs.info = tfs.handle.TorrentFile()
}

func (tfs *TorrentFS) HasTorrentInfo() bool {
    return tfs.info != nil
}

func (tfs *TorrentFS) TorrentInfo() lt.TorrentInfo {
    for tfs.info == nil {
        time.Sleep(100 * time.Millisecond)
    }
    return tfs.info
}

func (tfs *TorrentFS) LoadFileProgress() {
    tfs.progresses = lt.NewStdVectorSizeType()
    tfs.handle.FileProgress(tfs.progresses, int(lt.TorrentHandlePieceGranularity))
}

func (tfs *TorrentFS) getFileDownloadedBytes(i int) (bytes int64) {
    defer func() {
        if res := recover(); res != nil {
            bytes = 0
        }
    }()
    bytes = tfs.progresses.Get(i)
    return
}

func (tfs *TorrentFS) Files() []*TorrentFile {
    info := tfs.TorrentInfo()
    files := make([]*TorrentFile, info.NumFiles())
    for i := 0; i < info.NumFiles(); i++ {
        file, _ := tfs.FileAt(i)
        file.downloaded = tfs.getFileDownloadedBytes(i)
        if file.Size() > 0 {
            file.progress = float32(file.downloaded)/float32(file.Size())
        }
        files[i] = file
    }
    return files
}

func (tfs *TorrentFS) SavePath() string {
    return tfs.handle.Status().GetSavePath()
}

func (tfs *TorrentFS) FileAt(index int) (*TorrentFile, error) {
    info := tfs.TorrentInfo()
    if index < 0 || index >= info.NumFiles() {
        return nil, ErrInvalidIndex
    }
    fileEntry := info.FileAt(index)
    path, _ := filepath.Abs(path.Join(tfs.SavePath(), fileEntry.GetPath()))
    return &TorrentFile{
        tfs: tfs,
        fileEntry: fileEntry,
        savePath: path,
        index: index,
    }, nil
}

func (tfs *TorrentFS) FileByName(name string) (*TorrentFile, error) {
    savePath, _ := filepath.Abs(path.Join(tfs.SavePath(), name))
    for _, file := range tfs.Files() {
        if file.SavePath() == savePath {
            return file, nil
        }
    }
    return nil, ErrFileNotFound
}

func (tfs *TorrentFS) Open(name string) (http.File, error) {
    if tfs.shuttingDown || !tfs.HasTorrentInfo() {
        return nil, ErrFileNotFound
    }
    if name == "/" {
        return &TorrentDir{tfs: tfs}, nil
    }
    return tfs.OpenFile(name)
}

func (tfs *TorrentFS) checkPriorities() {
    for index, priority := range tfs.priorities {
        if priority == 0 {
            continue
        }
        found := false
        for _, f := range tfs.openedFiles {
            if f.index == index {
                found = true
                break
            }
        }
        if !found {
            tfs.setPriority(index, 0)
        }
    }
}

func (tfs *TorrentFS) OpenFile(name string) (tf *TorrentFile, err error) {
    tf, err = tfs.FileByName(name)
    if err != nil {
        return
    }
    tfs.fileCounter++
    tf.num = tfs.fileCounter
    tf.log("Opening %s...", tf.Name())
    tfs.setPriority(tf.index, 6)
    startPiece, _ := tf.Pieces()
    tf.waitForPiece(startPiece)
    tfs.lastOpenedFile = tf
    tfs.addOpenedFile(tf)
    tfs.checkPriorities()
    return
}

func (tf *TorrentFile) SavePath() string {
    return tf.savePath
}

func (tf *TorrentFile) Index() int {
    return tf.index
}

func (tf *TorrentFile) Downloaded() (int64) {
    return tf.downloaded
}

func (tf *TorrentFile) Progress() (float32) {
    return tf.progress
}

func (tf *TorrentFile) FilePtr() (*os.File, error) {
    var err error
    if tf.closed {
        return nil, io.EOF
    }
    if tf.filePtr == nil {
        for {
            _, err = os.Stat(tf.savePath)
            if err == nil {
                break
            }
            time.Sleep(100 * time.Millisecond)
        }
        tf.filePtr, err = os.Open(tf.savePath)
    }
    return tf.filePtr, err
}

func (tf *TorrentFile) log(message string, v ...interface {}) {
    args := append([]interface{}{tf.num}, v...)
    log.Printf("[%d] "+message+"\n", args...)
}

func (tf *TorrentFile) Pieces() (int, int) {
    startPiece, _ := tf.pieceFromOffset(1)
    endPiece, _ := tf.pieceFromOffset(tf.Size() - 1)
    return startPiece, endPiece
}

func (tf *TorrentFile) SetPriority(priority int) {
    tf.tfs.setPriority(tf.index, priority)
}

func (tf *TorrentFile) Stat() (fileInfo os.FileInfo, err error) {
    return tf, nil
}

func (tf *TorrentFile) readOffset() (offset int64) {
    offset, _ = tf.filePtr.Seek(0, os.SEEK_CUR)
    return
}

func (tf *TorrentFile) havePiece(piece int) bool {
    return tf.tfs.handle.HavePiece(piece)
}

func (tf *TorrentFile) pieceLength() int {
    return tf.tfs.info.PieceLength()
}

func (tf *TorrentFile) pieceFromOffset(offset int64) (int, int) {
    pieceLength := int64(tf.tfs.info.PieceLength())
    piece := int((tf.Offset() + offset) / pieceLength)
    pieceOffset := int((tf.Offset() + offset) % pieceLength)
    return piece, pieceOffset
}

func (tf *TorrentFile) Offset() int64 {
    return tf.fileEntry.GetOffset()
}

func (tf *TorrentFile) waitForPiece(piece int) error {
    if !tf.havePiece(piece) {
        tf.log("Waiting for piece %d", piece)
        //tf.tfs.handle.PrioritizePiece(piece)
        tf.tfs.handle.PiecePriority(piece, 6)
        tf.tfs.handle.SetPieceDeadline(piece, 0)
    }
    for !tf.havePiece(piece) {
        if tf.tfs.handle.PiecePriority(piece).(int) == 0 || tf.closed {
            return io.EOF
        }
        time.Sleep(100 * time.Millisecond)
    }
    _, endPiece := tf.Pieces()
    if piece < endPiece && !tf.havePiece(piece+1) {
        tf.tfs.handle.PiecePriority(piece+1, 6)
        tf.tfs.handle.SetPieceDeadline(piece+1, 0)
    }
    return nil
}

func (tf *TorrentFile) Close() (err error) {
    if tf.closed {
        return
    }
    tf.log("Closing %s...", tf.Name())
    tf.tfs.removeOpenedFile(tf)
    tf.closed = true
    if tf.filePtr != nil {
        err = tf.filePtr.Close()
    }
    return
}

func (tf *TorrentFile) ShowPieces() {
    pieces := tf.tfs.handle.Status().GetPieces()
    startPiece, endPiece := tf.Pieces()
    str := ""
    for i := startPiece; i <= endPiece; i++ {
        if pieces.GetBit(i) == false {
            str += "-"
        } else {
            str += "#"
        }
    }
    tf.log(str)
}

func (tf *TorrentFile) Read(data []byte) (int, error) {
    filePtr, err := tf.FilePtr()
    if err != nil {
        return 0, err
    }
    toRead := len(data)
    if toRead > tf.pieceLength() {
        toRead = tf.pieceLength()
    }
    readOffset := tf.readOffset()
    startPiece, _ := tf.pieceFromOffset(readOffset)
    endPiece, _ := tf.pieceFromOffset(readOffset + int64(toRead))
    for i := startPiece; i <= endPiece; i++ {
        if err := tf.waitForPiece(i); err != nil {
            return 0, err
        }
    }
    tmpData := make([]byte, toRead)
    read, err := filePtr.Read(tmpData)
    if err == nil {
        copy(data, tmpData[:read])
    }
    return read, err
}

func (tf *TorrentFile) Seek(offset int64, whence int) (newOffset int64, err error) {
    filePtr, err := tf.FilePtr()
    if err != nil {
        return
    }
    if whence == os.SEEK_END {
        offset = tf.Size()-offset
        whence = os.SEEK_SET
    }
    newOffset, err = filePtr.Seek(offset, whence)
    if err != nil {
        return
    }
    tf.log("Seeking to %d/%d", newOffset, tf.Size())
    return
}

func (tf *TorrentFile) Readdir(int) ([]os.FileInfo, error) {
    return make([]os.FileInfo, 0), nil
}

func (tf *TorrentFile) Name() string {
    return tf.fileEntry.GetPath()
}

func (tf *TorrentFile) Size() int64 {
    return tf.fileEntry.GetSize()
}

func (tf *TorrentFile) Mode() os.FileMode {
    return 0
}

func (tf *TorrentFile) ModTime() time.Time {
    return time.Unix(int64(tf.fileEntry.GetMtime()), 0)
}

func (tf *TorrentFile) IsDir() bool {
    return false
}

func (tf *TorrentFile) Sys() interface{} {
    return nil
}

func (tf *TorrentFile) IsComplete() bool {
    return tf.downloaded == tf.Size()
}

func (td *TorrentDir) Close() error {
    return nil
}

func (td *TorrentDir) Read([]byte) (int, error) {
    return 0, io.EOF
}

func (td *TorrentDir) Readdir(count int) (files []os.FileInfo, err error) {
    info := td.tfs.TorrentInfo()
    totalFiles := info.NumFiles()
    read := &td.entriesRead
    toRead := totalFiles-*read
    if count >= 0 && count < toRead {
        toRead = count
    }
    files = make([]os.FileInfo, toRead)
    for i := 0; i < toRead; i++ {
        files[i], err = td.tfs.FileAt(*read)
        *read++
    }
    return
}

func (td *TorrentDir) Seek(int64, int) (int64, error) {
    return 0, nil
}

func (td *TorrentDir) Stat() (os.FileInfo, error) {
    return td, nil
}

func (td *TorrentDir) Name() string {
    return "/"
}

func (td *TorrentDir) Size() int64 {
    return 0
}

func (td *TorrentDir) Mode() os.FileMode {
    return os.ModeDir
}

func (td *TorrentDir) ModTime() time.Time {
    return time.Now()
}

func (td *TorrentDir) IsDir() bool {
    return true
}

func (td *TorrentDir) Sys() interface {} {
    return nil
}
