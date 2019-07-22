# Concurrent Cached File Server
This was the final project done for the Machine Structures course taken at UC Berkeley. **It is a file server that features a cache which can efficiently handle thousands of concurrent file requests.**

## Setup and Execution

1) Install Golang [here](https://golang.org/doc/install).

2) Install the userlib for this file server by running: `go get github.com/Daniel-VDM/Concurrent-Cached-File-Server-Userlib`

3) Clone the project to the following directory: `$GOPATH/src/github.com/Daniel-VDM`
> Beware of the `$gopath`, it is important in Golang that the defined file structure is used.
> Note that this can be done with the following command: `go get github.com/Daniel-VDM/Concurrent-Cached-File-Server`

4) Run the server by running: `go run server.go`

Here are the run options:
```
  -c int
        Number of bytes to allow in the cache. (default 1000000)
  -d string
        The directory which the files are hosted in. (default "public_html/")
  -l    Log debugging messages.
  -p int
        Port to listen for HTTP requests (default port 8080). (default 8080)
  -t int
        Default timeout (in seconds) to wait before returning an error. (default 2)
```

> Note that file requests for `/cache/` will return cache information and file requests for `/cache/clear/` will clear the cache.

## Implementation Details
First of all, it can handle numerous concurrent requests. 

Also, any requests for a directory will get defaulted to the `index.html` file within said that directory. So for example `./test/` is really a request for `./test/index.html`.

Next, all file requests path will be sanitized. That is, '/../', '\/', or '//' tokens will get turned into a single '/' before requesting the file. This mitigates directory traversal attacks.

Lastly, the cache will exert the following behavior:
* It performs correctly for an arbitrary number of requests at the same time.
* It never goes over the specified capacity. 
* It is fully associative and uses random eviction. Note that it does not evict if the file in question cannot fit in the cache. 
* The size of the file is only based on the size of the data. This means the size does NOT include the cache entry structure or the filename size.
* If a file read responds with an error, it does NOT cache the error.
* If two requests come in where the first has to fetch the file from disk while the second has to get the file from the cache, the disk request does not block the cache request.
* It has concurrent disk reads.
* If a file read takes longer than the time specified, it returns a timeout error right after the timeout time has passed. If it then receives the file back after returning a timeout, it inserts the file into the cache
* Clear cache command will reinitiate the cache.

