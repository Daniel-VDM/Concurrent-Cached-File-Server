package main

import (
	"flag"
	"fmt"
	"github.com/Daniel-VDM/Concurrent-Cached-File-Server-Userlib"
	"log"
	"net/http"
	"strings"
	"time"
)

/**
 * Debugging utility to toggle logging.
 */
var isLogging bool

func debugLog(msg string) {
	if isLogging {
		log.Println(msg)
	}
}

/**
 * Globals Variables for server.
 */
var (
	port       int
	capacity   int
	timeout    int
	workingDir string
)

/**
 * The handler for every request other than cache specific requests.
 */
func handler(w http.ResponseWriter, r *http.Request) {
	debugLog(fmt.Sprintf(">> Requesting (raw): '%v'", r.URL.Path))
	startTime := time.Now()

	response := getFile(r.URL.Path)
	if response.responseError != nil {
		errStr := response.responseError.Error()
		debugLog(fmt.Sprintf("<< [ERROR] Returned: '%v' | It took: %v | MSG: %v",
			response.filename, time.Now().Sub(startTime).String(),
			strings.Replace(errStr, "\n", "\\n", -1)))
		if errStr == userlib.TimeoutString {
			http.Error(w, errStr, userlib.TIMEOUTERRORCODE)
		} else {
			http.Error(w, errStr, userlib.FILEERRORCODE)
		}
		return
	}
	debugLog(fmt.Sprintf("<< Returned: '%v' | It took: %v",
		response.filename, time.Now().Sub(startTime).String()))

	w.Header().Set(userlib.ContextType, userlib.GetContentType(response.filename))
	w.WriteHeader(userlib.SUCCESSCODE)
	_, _ = w.Write(*response.responseData)
}

/**
 * The handler for cache status.
 */
func cacheHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(userlib.ContextType, userlib.GetContentType("AriaKillsTheNightKing.txt"))
	w.WriteHeader(userlib.SUCCESSCODE)
	_, _ = w.Write([]byte(getCacheStatus()))
}

/**
 * The handler for requests to clear/restart the cache.
 */
func cacheClearHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(userlib.ContextType, userlib.GetContentType("IronManDies.txt"))
	w.WriteHeader(userlib.SUCCESSCODE)
	_, _ = w.Write([]byte(cacheClear()))
}

/**
 * Internal structure and channel for file communication between threads.
 */
type fileResponse struct {
	filename      string
	responseData  *[]byte
	responseError error
	responseChan  chan *fileResponse
}

type fileRequest struct {
	filename string
	response chan *fileResponse
}

var fileChan = make(chan *fileRequest)

/**
 * Wrapper function sanitizes the filepath/filename and gets the file from cache.
 */
func getFile(filename string) (response *fileResponse) {
	for strings.Contains(filename, "/../") ||
		strings.Contains(filename, "\\/") ||
		strings.Contains(filename, "//") {
		filename = strings.Replace(filename, "/../", "/", -1)
		filename = strings.Replace(filename, "\\/", "/", -1)
		filename = strings.Replace(filename, "//", "/", -1)
	}
	if filename[len(filename)-1] == '/' {
		filename += "index.html"
	}
	request := fileRequest{"./" + filename[1:], make(chan *fileResponse)}
	fileChan <- &request
	return <-request.response
}

/**
 * Internal variables, structures and channels for the cache.
 */
var (
	cacheCapacityChan = make(chan chan string)
	cacheCloseChan    = make(chan bool)
	cacheOpChan       = make(chan *cacheOp)
	WRITE             = 0
	READ              = 1
	STATS             = 2
)

type cacheEntry struct {
	filename string
	data     *[]byte
	valid    bool
	size     int
	count    int
}

type cache struct {
	table map[string]*cacheEntry
	size  int // Size of ALL data (values) in bytes
}

type cacheOp struct {
	op       int // 0 = Write, 1 = Read, 2 = Stats
	filename string
	data     *[]byte
	readChan chan *cacheEntry
}

/**
 * This function requests and returns the cache status.
 */
func getCacheStatus() (response string) {
	responseChan := make(chan string)
	cacheCapacityChan <- responseChan
	return <-responseChan
}

/**
 * This function toggles a cache clear and returns a message when cleared.
 * NOTE: Concurrent cache clears are NOT supported.
 */
func cacheClear() (response string) {
	cacheCloseChan <- true
	<-cacheCloseChan // Wait until the cache is closed before restarting
	go operateCache()
	return userlib.CacheCloseMessage
}

/**
 * This thread is spawned once from the main cache thread (operateCache) during its initialization.
 * It handles all map operations for the cache, thus avoiding any data races.
 */
func cacheMapOperator(close chan bool) {
	cache := cache{make(map[string]*cacheEntry), 0}
	for {
		//Debugging
		//keys := make([]string, 0, len(cache.table))
		//for k := range cache.table {
		//	keys = append(keys, k)
		//}
		//fmt.Printf("Len (%v): %v\n", len(keys), keys)

		select { // Drain the close channel first.
		case <-close:
			return
		default:
		}

		select {
		case <-close:
			return
		case cacheOp := <-cacheOpChan:
			switch cacheOp.op {
			case WRITE:
				if len(*cacheOp.data) > capacity {
					continue // Don't destroy cache if cache can't fit data.
				}
				debugLog(fmt.Sprintf("\t\t\tAdding %v to cache", cacheOp.filename))
				if entry, ok := cache.table[cacheOp.filename]; ok {
					delete(cache.table, cacheOp.filename)
					cache.size -= len(*entry.data)
				}
				for k := range cache.table {
					if cache.size+len(*cacheOp.data) <= capacity {
						break
					}
					delEntry := cache.table[k]
					delete(cache.table, delEntry.filename)
					cache.size -= len(*delEntry.data)
				}
				cache.table[cacheOp.filename] = &cacheEntry{cacheOp.filename,
					cacheOp.data, true, -1, -1}
				cache.size += len(*cacheOp.data)
			case READ:
				entry, ok := cache.table[cacheOp.filename]
				if !ok {
					entry = &cacheEntry{"", nil, false, -1, -1}
				}
				cacheOp.readChan <- entry
			case STATS:
				entry := &cacheEntry{"", nil, false,
					cache.size, len(cache.table)}
				cacheOp.readChan <- entry
			}
		}
	}
}

/**
 * This thread is spawned from the main cache thread (operateCache) every time the
 * cache misses. This enables concurrent file reads. Also, this thread returns a
 * request to the request's channel once it reads AND caches data (or when timeout occurs).
 * NOTE: Thread stays active until the read data is cached (even when timing out).
 */
func cacheMiss(fileReq *fileRequest) {
	processedChan := make(chan bool, 1)

	go func() {
		data, err := userlib.ReadFile(workingDir, fileReq.filename)
		if err != nil {
			// Don't cache if it's a file error.
			fileReq.response <- &fileResponse{fileReq.filename, &data,
				fmt.Errorf(userlib.FILEERRORMSG), fileReq.response}
		} else {
			cacheOpChan <- &cacheOp{WRITE, fileReq.filename, &data, nil}
			fileReq.response <- &fileResponse{fileReq.filename, &data,
				nil, fileReq.response}
		}
		processedChan <- true
	}()

	select {
	case <-processedChan:
	case <-time.After(time.Second * time.Duration(timeout)):
		debugLog(fmt.Sprintf("\t\t[!!] Time out: %v", fileReq.filename))
		fileReq.response <- &fileResponse{fileReq.filename, nil,
			fmt.Errorf(userlib.TimeoutString), fileReq.response}
		<-processedChan // Don't close thread until read file is cached.
	}
}

/**
 * This thread handles all cache file requests at runtime and spawns off
 * all of the necessary threads at runtime.
 */
func operateCache() {
	mapOpCloseChan := make(chan bool)
	go cacheMapOperator(mapOpCloseChan)

	for {
		select {
		case fileReq := <-fileChan:
			cacheOp := cacheOp{READ, fileReq.filename,
				nil, make(chan *cacheEntry)}
			cacheOpChan <- &cacheOp
			cacheEntry := <-cacheOp.readChan
			if cacheEntry.valid {
				debugLog(fmt.Sprintf("\t[*]Hit: %v", fileReq.filename))
				fileReq.response <- &fileResponse{cacheEntry.filename, cacheEntry.data,
					nil, fileReq.response}
			} else {
				debugLog(fmt.Sprintf("\t[!]Miss: %v", fileReq.filename))
				go cacheMiss(fileReq)
			}
		case cacheReq := <-cacheCapacityChan:
			cacheOp := cacheOp{STATS, "", nil, make(chan *cacheEntry)}
			cacheOpChan <- &cacheOp
			entry := <-cacheOp.readChan
			cacheReq <- fmt.Sprintf(userlib.CapacityString, entry.count, entry.size, capacity)
		case cacheClose := <-cacheCloseChan:
			if cacheClose {
				mapOpCloseChan <- true
				for {
					select {
					case <-cacheOpChan: // Flush any remaining cache operations.
					default:
						cacheCloseChan <- false
						return
					}
				}
			}
		}
	}
}

func main() {
	flag.IntVar(&port, "p", 8080, "Port to listen for HTTP requests (default port 8080).")
	flag.IntVar(&capacity, "c", 1000000, "Number of bytes to allow in the cache.")
	flag.IntVar(&timeout, "t", 2, "Default timeout (in seconds) to wait before returning an error.")
	flag.StringVar(&workingDir, "d", "public_html/", "The directory which the files are hosted in.")
	flag.BoolVar(&isLogging, "l", false, "Log debugging messages.")
	flag.Parse()

	fmt.Printf("Server starting, port: %v, cache size: %v, timout: %v, working dir: '%s'\n",
		port, capacity, timeout, workingDir)
	serverString := fmt.Sprintf(":%v", port)

	http.HandleFunc("/", handler)
	http.HandleFunc("/cache/", cacheHandler)
	http.HandleFunc("/cache/clear/", cacheClearHandler)

	go operateCache()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Fatal(http.ListenAndServe(serverString, nil))
}
