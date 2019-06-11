package main

import (
	"bytes"
	"fmt"
	"github.com/Daniel-VDM/Concurrent-Cached-File-Server-Userlib"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

/*
 *	This is a hacky method to make the handler think that it is talking to the web response.
 */
type ResponseWriterTester struct {
	http.ResponseWriter
	header     http.Header
	data       []byte
	statusCode int
}

func (r *ResponseWriterTester) Header() http.Header {
	return r.header
}

func (r *ResponseWriterTester) Write(d []byte) (int, error) {
	r.data = append(r.data, d...)
	return len(d), nil
}

func (r *ResponseWriterTester) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}

func genResponseTestWriter() *ResponseWriterTester {
	return &ResponseWriterTester{header: http.Header{}}
}

func genRequestUrl(urlpath string) *http.Request {
	return &http.Request{URL: &url.URL{Path: urlpath}}
}

func clearCache() {
	resp := genResponseTestWriter()
	req := genRequestUrl(cacheCloseUrl)
	cacheClearHandler(resp, req)
}

var cacheUrl = "/cache"
var cacheCloseUrl = "/cache/close"

func validateFileResponse(readName, expectedName string, dataToBeRead []byte, resp *ResponseWriterTester, expectedCode int, t *testing.T) (failed bool) {
	// Finally we validate if we read the correct filename.
	if expectedName != "" && expectedName != readName {
		failed = true
		t.Errorf("The path which was passed in was not correct! Expected: (%s), Actual: (%s)", string(expectedName), string(readName))
	}
	// We will also assert that we have read the correct bytes just to be certain that everything was saved and forwarded correctly.
	if dataToBeRead != nil && !bytes.Equal(dataToBeRead, resp.data) {
		failed = true
		t.Errorf("The data that was received was not what was expected! Expected (%s), Actual (%s)", string(dataToBeRead), string(resp.data))
	}
	// Next we check to see if the header has the correct value for the context type.
	if resp.header.Get(userlib.ContextType) != userlib.GetContentType(expectedName) {
		failed = true
		t.Errorf("Received the wrong context type for the file. Expected: (%s), Actual: (%s)", string(userlib.GetContentType(expectedName)), string(resp.header.Get(userlib.ContextType)))
	}
	// We finally wanna check that we set the correct return code.
	if resp.statusCode != expectedCode {
		failed = true
		t.Errorf("Received the wrong status code! Expected: (%v), Actual: (%v)", expectedCode, resp.statusCode)
	}
	return failed
}

func validateBadFile(badName string, secTimeout int, t *testing.T) (failed bool) {
	resp := requestFile(badName, secTimeout, t)
	dataToBeRead := []byte(userlib.FILEERRORMSG + "\n")
	// We will also assert that we have read the correct bytes just to be certain that everything was saved and forwarded correctly.
	if dataToBeRead != nil && !bytes.Equal(dataToBeRead, resp.data) {
		failed = true
		t.Errorf("The data that was received was not what was expected! Expected (%s), Actual (%s)", string(dataToBeRead), string(resp.data))
	}
	// We finally wanna check that we set the correct return code.
	if resp.statusCode != userlib.FILEERRORCODE {
		failed = true
		t.Errorf("Received the wrong status code! Expected: (%v), Actual: (%v)", userlib.FILEERRORCODE, resp.statusCode)
	}
	return failed
}

func validateTimeout(resp *ResponseWriterTester, t *testing.T) (failed bool) {
	if !bytes.Equal(resp.data, []byte(userlib.TimeoutString+"\n")) {
		failed = true
		t.Errorf("I did not receive the timeout string! Expected: (%s), Actual: (%s)!", userlib.TimeoutString+"\n", string(resp.data))
	}
	return failed
}

func validateCacheSize(items, size int, t *testing.T) (failed bool) {
	resp := genResponseTestWriter()
	req := genRequestUrl(cacheUrl)
	cacheHandler(resp, req)
	correctCacheResponse := fmt.Sprintf(userlib.CapacityString, items, size, capacity)
	if !bytes.Equal(resp.data, []byte(correctCacheResponse)) {
		failed = true
		t.Errorf("The expected cache data was not correct! Expected: (%s), Actual: (%s)", correctCacheResponse, string(resp.data))
	}
	return failed
}

func validateCacheNotExceeded(t *testing.T) (failed bool) {
	resp := genResponseTestWriter()
	req := genRequestUrl(cacheUrl)
	rgx := regexp.MustCompile(`\[(.*?)\]`)
	cacheHandler(resp, req)
	rs := rgx.FindStringSubmatch(string(resp.data))
	if len(rs) < 2 {
		failed = true
		t.Errorf("Could not read the current cache capacity! Got back: (%s)", string(resp.data))
		return failed
	}
	curCap, err := strconv.ParseInt(rs[1], 10, 32)
	if err != nil {
		failed = true
		t.Error(err)
		return failed
	}
	if curCap > int64(capacity) {
		failed = true
		t.Errorf("The capacity of the cache has been exceeded! Expected max: (%v), Actual: (%v)", capacity, curCap)
	}
	return failed
}

func validateNumberOfReads(expected, count uint64, t *testing.T) (failed bool) {
	if expected != count {
		failed = true
		t.Errorf("An incorrect number of reads occurred.")
	}
	return failed
}

func requestFile(filename string, secTimeout int, t *testing.T) *ResponseWriterTester {
	resp := genResponseTestWriter()
	req := genRequestUrl(filename)
	c := make(chan bool)
	go func(chan bool) {
		handler(resp, req)
		c <- true
	}(c)
	select {
	case <-c: // This means we did not time out.
	case <-time.After(time.Duration(secTimeout+1) * time.Second):
		t.Errorf("The handler took too long to respond!")
		t.FailNow()
	}
	return resp
}

var launched = false

//func init() {
//	go operateCache()
//}
func launchCache() {
	if launched == false {
		launched = true
		go operateCache()
	}
}

// ============ Sanity Tests ============

func TestSanityFileTest(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// This is the filename which will be passed into the handler as a url.
	name := "/cs61c.html"
	// For this test, we expect the only change to the file name to be adding a dot in front of it since there is nothing else to replace.
	expectedName := "." + name
	// I keep a dummy readName variable which will be set by the custom userlib function I defined below.
	readName := ""
	// This is the data which that fake file will contain.
	dataToBeRead := []byte("CS61C is the best class in the world! Emperor Nick shall reign supreme.")
	// This is bad data which will be read if the filename is not correct.
	badData := []byte("CS61C is the worst class ever!")
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Set the global readName variable to what we read for an error check later.
		readName = filename
		// If we get the expected name, we will return the correct data.
		if filename == expectedName {
			data = dataToBeRead
		} else {
			// Otherwise we return the bad data.
			data = badData
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	validateFileResponse(readName, expectedName, dataToBeRead, resp, userlib.SUCCESSCODE, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanityFileCheckSize(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// This is the filename which will be passed into the handler as a url.
	name := "/README.md"
	// For this test, we expect the only change to the file name to be adding a dot in front of it since there is nothing else to replace.
	expected_name := "." + name
	// I keep a dummy read_name variable which will be set by the custom userlib function I defined below.
	read_name := ""
	// This is the data which that fake file will contain.
	data_to_be_read := []byte("CS61C is the best class in the world! Emperor Nick shall reign supreme.")
	// This is bad data which will be read if the filename is not correct.
	bad_data := []byte("CS61C is the worst class ever!")
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	// We need to count the number of file reads we get.
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		// Set the global read_name variable to what we read for an error check later.
		read_name = filename
		// If we get the expected name, we will return the correct data.
		if filename == expected_name {
			data = data_to_be_read
		} else {
			// Otherwise we return the bad data.
			data = bad_data
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	validateFileResponse(read_name, expected_name, data_to_be_read, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(data_to_be_read), t)
	validateNumberOfReads(1, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanityFileDoubleRead(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// This is the filename which will be passed into the handler as a url.
	name := "/README.md"
	// For this test, we expect the only change to the file name to be adding a dot in front of it since there is nothing else to replace.
	expected_name := "." + name
	// I keep a dummy read_name variable which will be set by the custom userlib function I defined below.
	read_name := ""
	// This is the data which that fake file will contain.
	data_to_be_read := []byte("CS61C is the best class in the world! Emperor Nick shall reign supreme.")
	// This is bad data which will be read if the filename is not correct.
	bad_data := []byte("CS61C is the worst class ever!")
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	// We need to count the number of file reads we get.
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		// Set the global read_name variable to what we read for an error check later.
		read_name = filename
		// If we get the expected name, we will return the correct data.
		if filename == expected_name {
			data = data_to_be_read
		} else {
			// Otherwise we return the bad data.
			data = bad_data
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	validateFileResponse(read_name, expected_name, data_to_be_read, resp, userlib.SUCCESSCODE, t)
	resp = genResponseTestWriter()
	handler(resp, req)
	validateFileResponse(read_name, expected_name, data_to_be_read, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(data_to_be_read), t)
	validateNumberOfReads(1, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanityReadCloseRead(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// This is the filename which will be passed into the handler as a url.
	name := "/README.md"
	// For this test, we expect the only change to the file name to be adding a dot in front of it since there is nothing else to replace.
	expected_name := "." + name
	// I keep a dummy read_name variable which will be set by the custom userlib function I defined below.
	read_name := ""
	// This is the data which that fake file will contain.
	data_to_be_read := []byte("CS61C is the best class in the world! Emperor Nick shall reign supreme.")
	// This is bad data which will be read if the filename is not correct.
	bad_data := []byte("CS61C is the worst class ever!")
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	// We need to count the number of file reads we get.
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		// Set the global read_name variable to what we read for an error check later.
		read_name = filename
		// If we get the expected name, we will return the correct data.
		if filename == expected_name {
			data = data_to_be_read
		} else {
			// Otherwise we return the bad data.
			data = bad_data
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	validateFileResponse(read_name, expected_name, data_to_be_read, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(data_to_be_read), t)
	if reads != 1 {
		t.Errorf("There should have been only one file read before we close.")
	}
	clearCache()
	validateCacheSize(0, 0, t)
	resp = genResponseTestWriter()
	req = genRequestUrl(name)
	handler(resp, req)
	validateFileResponse(read_name, expected_name, data_to_be_read, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(data_to_be_read), t)
	validateNumberOfReads(2, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanity404(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 100
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	// This is the filename which will be passed into the handler as a url.
	name := "/IDONTEXIST.txt"
	// For this test, we expect the only change to the file name to be adding a dot in front of it since there is nothing else to replace.
	expectedName := "." + name
	// This is the data which that fake file will contain.
	dataToBeRead := []byte("CS61C is the best class in the world! Emperor Nick shall reign supreme.")
	// This is bad data which will be read if the filename is not correct.
	badData := []byte("CS61C is the worst class ever!")
	// We need to count the number of file reads we get.
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		// If we get the expected name, we will return the correct data.
		if filename == expectedName {
			data = dataToBeRead
		} else {
			// Otherwise we return the bad data.
			data = badData
		}
		err = fmt.Errorf("the file does not exist")
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	validateBadFile(name, secTimeout, t)
	validateCacheSize(0, 0, t)
	validateBadFile(name, secTimeout, t)
	validateCacheSize(0, 0, t)
	validateNumberOfReads(2, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanityFileType(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 100
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	readName := ""
	fData := []byte("data")
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		readName = filename
		data = fData
		return
	})
	name := "/exam.htm"
	resp := requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	name = "/exam.html"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	name = "/exam.jpeg"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	name = "/exam.jpg"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	name = "/exam.png"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	name = "/exam.css"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	name = "/exam.js"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	name = "/exam.pdf"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	name = "/exam.pdx"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	name = "/exam.txt"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	name = "/exam.txt"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, "."+name, fData, resp, userlib.SUCCESSCODE, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

// ============ End of Sanity Tests ============

// ============ Implementation Tests ============
func TestImplIndexFileTests(t *testing.T) {
	secCap := 200
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	// This is the filename which will be passed into the handler as a url.
	name := "/"
	// For this test, we expect the only change to the file name to be adding a dot in front of it since there is nothing else to replace.
	expectedName := "." + name + "index.html"
	// This is the data which that fake file will contain.
	dataToBeRead := []byte("CS61C is the best class in the world! Emperor Nick shall reign supreme.")
	// This is bad data which will be read if the filename is not correct.
	badData := []byte("CS61C is the worst class ever!")
	// We need to count the number of file reads we get.
	var reads uint64 = 0
	readName := ""
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		readName = filename
		// If we get the expected name, we will return the correct data.
		if filename == expectedName {
			data = dataToBeRead
		} else {
			// Otherwise we return the bad data.
			data = badData
		}
		return
	})
	resp := requestFile(name, secTimeout, t)
	validateFileResponse(readName, expectedName, dataToBeRead, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(dataToBeRead), t)
	name = "/best/class/ever/"
	expectedName = "./best/class/ever/index.html"
	resp = requestFile(name, secTimeout, t)
	validateCacheSize(2, 2*len(dataToBeRead), t)
	validateFileResponse(readName, expectedName, dataToBeRead, resp, userlib.SUCCESSCODE, t)
	name = "/best/class/ever"
	expectedName = "./best/class/ever"
	resp = requestFile(name, secTimeout, t)
	validateFileResponse(readName, expectedName, dataToBeRead, resp, userlib.SUCCESSCODE, t)
	validateNumberOfReads(3, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

// ============ End of Implementation Tests ============

// ============ File String Sanitization Tests ============
func TestSanitizationDoubleSlash(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	name := "//test.61c"
	expected := "./test.61c"
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		if filename != expected {
			t.Errorf("The filename does not match the expected file name. Expected (%s), Actual (%s)", expected, filename)
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "/file//test.61c"
	req = genRequestUrl(name)
	expected = "./file/test.61c"
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "//file//tool/test.61c"
	req = genRequestUrl(name)
	expected = "./file/tool/test.61c"
	handler(resp, req)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanitizationVSlashPattern(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	name := "\\/Vtest.61c"
	expected := "./Vtest.61c"
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		if filename != expected {
			t.Errorf("The filename does not match the expected file name. Expected (%s), Actual (%s)", expected, filename)
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "/file\\/Vtest.61c"
	req = genRequestUrl(name)
	expected = "./file/Vtest.61c"
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "\\/file\\/tool/Vtest.61c"
	req = genRequestUrl(name)
	expected = "./file/tool/Vtest.61c"
	handler(resp, req)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanitizationPreviousDirectory(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	name := "/../Ptest.61c"
	expected := "./Ptest.61c"
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		if filename != expected {
			t.Errorf("The filename does not match the expected file name. Expected (%s), Actual (%s)", expected, filename)
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "/file/../Ptest.61c"
	req = genRequestUrl(name)
	expected = "./file/Ptest.61c"
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "/../file/../tool/Ptest.61c"
	req = genRequestUrl(name)
	expected = "./file/tool/Ptest.61c"
	handler(resp, req)
	resp = genResponseTestWriter()
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanitizationSimpleCombinations(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	name := "//../SCtest.61c"
	expected := "./SCtest.61c"
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		if filename != expected {
			t.Errorf("The filename does not match the expected file name. Expected (%s), Actual (%s)", expected, filename)
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "//file/../SCtest.61c"
	req = genRequestUrl(name)
	expected = "./file/SCtest.61c"
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "/../file\\//../tool/SCtest.61c"
	req = genRequestUrl(name)
	expected = "./file/tool/SCtest.61c"
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "/..//..///../file/..//..//..///../\\//..//..//../exams//\\/SCtest.61c"
	req = genRequestUrl(name)
	expected = "./file/exams/SCtest.61c"
	handler(resp, req)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanitizationSimpleSameEndFile(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	name := "//../test.61c"
	expected := "./test.61c"
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	shouldReadFile := true
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		if filename != expected {
			t.Errorf("The filename does not match the expected file name. Expected (%s), Actual (%s)", expected, filename)
		}
		if shouldReadFile == false {
			t.Errorf("The filename which should have been the same was not cached!")
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	shouldReadFile = false
	resp = genResponseTestWriter()
	name = "//../../test.61c"
	req = genRequestUrl(name)
	expected = "./test.61c"
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "/../\\//..//test.61c"
	req = genRequestUrl(name)
	expected = "./test.61c"
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "/..//..///../\\/..//..//..///../\\//..//..//..///\\/test.61c"
	req = genRequestUrl(name)
	expected = "./test.61c"
	handler(resp, req)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanitizationLongReplace(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	name := "test.61c"
	name = strings.Repeat("\\", 200000) + "/" + name
	expected := "./test.61c"
	//expected := "./" + strings.Repeat("dir/", int(math.Floor(float64(times) / float64(len(comb))))) + "test.61c"
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	shouldReadFile := true
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		if filename != expected {
			t.Errorf("The filename does not match the expected file name. Expected (%s), Actual (%s)", expected, filename)
		}
		if shouldReadFile == false {
			t.Errorf("The filename which should have been the same was not cached!")
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestSanitizationComplexCombinations(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	name := "//../cCtest.61c"
	expected := "./cCtest.61c"
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(name)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		if filename != expected {
			t.Errorf("The filename does not match the expected file name. Expected (%s), Actual (%s)", expected, filename)
		}
		return
	})
	// We finally will call the handler. This is where we will get the data from the cache and or filesystem depending
	// on if it is in the cache. It will be stored in the dummy Response Writer which was created above.
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "//file/..//cCtest.61c"
	req = genRequestUrl(name)
	expected = "./file/cCtest.61c"
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "\\/../file\\/../tool/cCtest.61c"
	req = genRequestUrl(name)
	expected = "./file/tool/cCtest.61c"
	handler(resp, req)
	resp = genResponseTestWriter()
	name = "/../..//../file/..\\\\\\\\//../..//\\//../\\/../../..\\//..\\//..\\//..\\//..\\//..\\/exams//\\/cCtest.61c"
	req = genRequestUrl(name)
	expected = "./file/exams/cCtest.61c"
	handler(resp, req)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

// ============ End of File String Sanitization Tests ============

// ============ Timeout Tests ============
func TestTimeoutSimple(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	badname := "/badfile.61c"
	goodname := "/goodfile.61c"
	gooddata := []byte("I am some really good data that is fun to watch and good to know!")
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := genResponseTestWriter()
	// We finally make the http request which the handler can understand.
	req := genRequestUrl(badname)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		if filename == ("." + goodname) {
			data = gooddata
		} else {
			// Inf loop.
			for filename != filename+"hahaha" {
			}
		}
		return
	})
	c := make(chan bool)
	go func(chan bool) {
		handler(resp, req)
		c <- true
	}(c)
	select {
	case <-c: // This means we did not time out.
	case <-time.After(time.Duration(secTimeout+1) * time.Second):
		t.Errorf("The handler took too long to respond!")
	}
	validateTimeout(resp, t)
	validateCacheSize(0, 0, t)
	goodreq := genRequestUrl(goodname)
	goodresp := genResponseTestWriter()
	handler(goodresp, goodreq)
	validateFileResponse("", "", gooddata, goodresp, userlib.SUCCESSCODE, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestTimeoutWithResponse(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	goodname := "/goodfile.61c"
	gooddata := []byte("I am some really good data that is fun to watch and good to know!")
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		time.Sleep(time.Duration(timeout*2) * time.Second)
		data = gooddata
		return
	})
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := requestFile(goodname, secTimeout, t)
	validateTimeout(resp, t)
	validateCacheSize(0, 0, t)
	time.Sleep(time.Duration(timeout)*time.Second + time.Millisecond*time.Duration(250))
	validateCacheSize(1, len(gooddata), t)
	resp2 := requestFile(goodname, secTimeout, t)
	validateFileResponse("", "", gooddata, resp2, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(gooddata), t)
	validateNumberOfReads(1, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestTimeoutValidateSingleResponse(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	goodname := "/goodfile.61c"
	gooddata := []byte("I am some really good data that is fun to watch and good to know!")
	var reads uint64 = 0
	done := make(chan bool)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		time.Sleep(time.Duration(timeout*2) * time.Second)
		data = gooddata
		done <- true
		return
	})
	// We need to set up a response writer which will be the dummy passed into the handler to make testing easier.
	resp := requestFile(goodname, secTimeout, t)
	validateTimeout(resp, t)
	validateCacheSize(0, 0, t)
	<-done
	time.Sleep(time.Duration(timeout)*time.Second + time.Millisecond*time.Duration(250))
	validateTimeout(resp, t)
	validateCacheSize(1, len(gooddata), t)
	resp2 := requestFile(goodname, secTimeout, t)
	validateFileResponse("", "", gooddata, resp2, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(gooddata), t)
	validateNumberOfReads(1, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestTimeoutNoResponseThenSecondHasResponse(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 1000
	secTimeout := 1
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	badname := "/badfile.61c"
	goodname := "/goodfile.61c"
	gooddata := []byte("I am some really good data that is fun to watch and good to know!")
	baddata := []byte("I am some really bad data that is fun to watch and good to know!")
	var reads uint64 = 0
	done := make(chan bool)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		atomic.AddUint64(&reads, 1)
		if filename == ("." + goodname) {
			data = gooddata
		} else {
			// Inf loop.
			switch reads {
			case 1:
				for filename != filename+"hahaha" {
				}
			case 2:
				time.Sleep(time.Duration(secTimeout+1) * time.Second)
				data = baddata
				done <- true
			default:
				t.Errorf("I made more requests that I should have had!")
			}
		}
		return
	})
	resp := requestFile(badname, secTimeout, t)
	validateTimeout(resp, t)
	validateCacheSize(0, 0, t)
	resp = requestFile(badname, secTimeout, t)
	validateTimeout(resp, t)
	validateCacheSize(0, 0, t)
	select {
	case <-done:
	case <-time.After(time.Duration((timeout+1)*2) * time.Second):
		t.Errorf("Failed to get my response!")
	}
	time.Sleep(time.Duration(100) * time.Millisecond)
	resp = requestFile(badname, secTimeout, t)
	validateFileResponse("", "", baddata, resp, userlib.SUCCESSCODE, t)

	goodreq := genRequestUrl(goodname)
	goodresp := genResponseTestWriter()
	handler(goodresp, goodreq)
	validateFileResponse("", "", gooddata, goodresp, userlib.SUCCESSCODE, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

// ============ End of Timeout Tests ============

// ============ Exact Capacity Tests ============
func TestExactCapacitySingle(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	numFiles := 10
	secCap := numFiles * 5
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		data = []byte(fmt.Sprintf("FID:%v", filename[2:]))
		return
	})
	for i := 0; i < numFiles; i++ {
		fid := fmt.Sprintf("%v", i)
		resp := requestFile("/"+fid, secTimeout, t)
		if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
			t.FailNow()
		}
	}
	validateCacheSize(numFiles, numFiles*5, t)
	for i := numFiles - 1; i >= 0; i-- {
		fid := fmt.Sprintf("%v", i)
		resp := requestFile("/"+fid, secTimeout, t)
		if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
			t.FailNow()
		}
	}
	validateCacheSize(numFiles, numFiles*5, t)
	validateNumberOfReads(uint64(numFiles), reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestExactCapacityMany(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	numFiles := 10
	secCap := numFiles * 5
	secTimeout := 2
	capacity = secCap
	iterations := 0x61c
	timeout = secTimeout
	workingDir = ""
	launchCache()
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		data = []byte(fmt.Sprintf("FID:%v", filename[2:]))
		return
	})
	for i := 0; i < numFiles; i++ {
		fid := fmt.Sprintf("%v", i)
		resp := requestFile("/"+fid, secTimeout, t)
		if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
			t.FailNow()
		}
	}
	validateCacheSize(numFiles, numFiles*5, t)
	for i := 0; i < iterations; i++ {
		if i%2 == 0 {
			for i := numFiles - 1; i >= 0; i-- {
				fid := fmt.Sprintf("%v", i)
				resp := requestFile("/"+fid, secTimeout, t)
				if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
					t.FailNow()
				}
			}
		} else {
			for i := numFiles - 1; i >= 0; i-- {
				fid := fmt.Sprintf("%v", i)
				resp := requestFile("/"+fid, secTimeout, t)
				if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
					t.FailNow()
				}

			}
		}
		if i%5 == 0 {
			for i := (numFiles - 1) / 2; i >= 0; i-- {
				fid := fmt.Sprintf("%v", i)
				resp := requestFile("/"+fid, secTimeout, t)
				if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
					t.FailNow()
				}

			}
			for i := 0; i < numFiles/2; i++ {
				fid := fmt.Sprintf("%v", i)
				resp := requestFile("/"+fid, secTimeout, t)
				if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
					t.FailNow()
				}

			}
		}
		if validateCacheSize(numFiles, numFiles*5, t) == true {
			t.FailNow()
		}
	}
	validateCacheSize(numFiles, numFiles*5, t)
	validateNumberOfReads(uint64(numFiles), reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

// ============ End of Exact Capacity Tests ============

// ============ Exceed Capacity Eviction Test ============
func TestExceedCapacityNoReadFileEviction(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	numFiles := 100
	iterations := 0x61c
	secCap := 14
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	allowRead := true
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		if !allowRead {
			t.Errorf("A read occurred which should not have occured!")
		}
		data = []byte(fmt.Sprintf("FID:%v", filename[2:]))
		return
	})
	for j := 0; j < iterations; j++ {
		for i := 0; i < numFiles; i++ {
			fid := fmt.Sprintf("%v", i)
			allowRead = true
			resp := requestFile("/"+fid, secTimeout, t)
			if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
				t.FailNow()
			}
			allowRead = false
			resp = requestFile("/"+fid, secTimeout, t)
			if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
				t.FailNow()
			}
			if validateCacheNotExceeded(t) == true {
				t.FailNow()
			}
		}
		for i := numFiles - 1; i >= 0; i-- {
			fid := fmt.Sprintf("%v", i)
			allowRead = true
			resp := requestFile("/"+fid, secTimeout, t)
			if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
				t.FailNow()
			}

			allowRead = false
			resp = requestFile("/"+fid, secTimeout, t)
			if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
				t.FailNow()
			}
			if validateCacheNotExceeded(t) == true {
				t.FailNow()
			}
		}
	}
	validateCacheNotExceeded(t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

// ============ End of Exceed Capacity Eviction Test ============

// ============ Exceed Capacity Tests ============
func TestExceedCapacityOneLargeFileThenAnotherLargeFile(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 30
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	largeFileName := "/largefile.61c"
	largeFileData := []byte("I am exactly 21 bytes")
	anotherLargeFileName := "/anotherlargefile.61c"
	anotherLargeFileData := []byte("1234567890123456789012345")
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		if filename == "."+largeFileName {
			data = largeFileData
		} else if filename == "."+anotherLargeFileName {
			data = anotherLargeFileData
		} else {
			data = []byte("BAD NAME")
		}
		return
	})
	resp := requestFile(largeFileName, secTimeout, t)
	validateFileResponse("", "", largeFileData, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(largeFileData), t)
	resp2 := requestFile(anotherLargeFileName, secTimeout, t)
	validateFileResponse("", "", anotherLargeFileData, resp2, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(anotherLargeFileData), t)
	validateNumberOfReads(2, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestExceedCapacityOneLargeFileThenOneByteOverNormal(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	largeFileData := []byte("I am very many bytes of data!")
	secCap := len(largeFileData) + 4
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	largeFileName := "/largefile.61c"
	anotherLargeFileName := "/anotherlargefile.61c"
	anotherLargeFileData := []byte("12345")
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		if filename == "."+largeFileName {
			data = largeFileData
		} else if filename == "."+anotherLargeFileName {
			data = anotherLargeFileData
		} else {
			data = []byte("BAD NAME")
		}
		return
	})
	resp := requestFile(largeFileName, secTimeout, t)
	validateFileResponse("", "", largeFileData, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(largeFileData), t)
	resp2 := requestFile(anotherLargeFileName, secTimeout, t)
	validateFileResponse("", "", anotherLargeFileData, resp2, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(anotherLargeFileData), t)
	validateNumberOfReads(2, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestExceedCapacityOneLargeFileThenOneByteOverWithOneByteFile(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	largeFileData := []byte("I am very many bytes of data!")
	secCap := len(largeFileData)
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	largeFileName := "/largefile.61c"
	anotherLargeFileName := "/anotherlargefile.61c"
	anotherLargeFileData := []byte("1")
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		if filename == "."+largeFileName {
			data = largeFileData
		} else if filename == "."+anotherLargeFileName {
			data = anotherLargeFileData
		} else {
			data = []byte("BAD NAME")
		}
		return
	})
	resp := requestFile(largeFileName, secTimeout, t)
	validateFileResponse("", "", largeFileData, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(largeFileData), t)
	resp2 := requestFile(anotherLargeFileName, secTimeout, t)
	validateFileResponse("", "", anotherLargeFileData, resp2, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(anotherLargeFileData), t)
	validateNumberOfReads(2, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestExceedCapacityOneLargeOneSmallOverByMany(t *testing.T) {
	largeFileData := []byte("I am very many bytes of data!")
	smallFileData := []byte("IM SMALL")
	secCap := len(largeFileData) + len(smallFileData) + 5
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	largeFileName := "/largefile.61c"
	anotherLargeFileName := "/anotherlargefile.61c"
	smallFileName := "/smallfile.61c"
	anotherLargeFileData := []byte("a very large file")
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		if filename == "."+largeFileName {
			data = largeFileData
		} else if filename == "."+anotherLargeFileName {
			data = anotherLargeFileData
		} else if filename == "."+smallFileName {
			data = smallFileData
		} else {
			data = []byte("BAD NAME")
		}
		return
	})
	resp := requestFile(largeFileName, secTimeout, t)
	validateFileResponse("", "", largeFileData, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(largeFileData), t)
	resp2 := requestFile(smallFileName, secTimeout, t)
	validateFileResponse("", "", smallFileData, resp2, userlib.SUCCESSCODE, t)
	validateCacheSize(2, len(largeFileData)+len(smallFileData), t)
	resp3 := requestFile(anotherLargeFileName, secTimeout, t)
	validateFileResponse("", "", anotherLargeFileData, resp3, userlib.SUCCESSCODE, t)
	validateCacheNotExceeded(t)
	validateNumberOfReads(3, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestExceedCapacityOneLargeOneSmallOverByOne(t *testing.T) {
	largeFileData := []byte("I am very many bytes of data!")
	smallFileData := []byte("IM SMALL")
	secCap := len(largeFileData) + len(smallFileData) + 5
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	largeFileName := "/largefile.61c"
	anotherLargeFileName := "/anotherlargefile.61c"
	smallFileName := "/smallfile.61c"
	anotherLargeFileData := []byte("123456")
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		if filename == "."+largeFileName {
			data = largeFileData
		} else if filename == "."+anotherLargeFileName {
			data = anotherLargeFileData
		} else if filename == "."+smallFileName {
			data = smallFileData
		} else {
			data = []byte("BAD NAME")
		}
		return
	})
	resp := requestFile(largeFileName, secTimeout, t)
	validateFileResponse("", "", largeFileData, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(largeFileData), t)
	resp2 := requestFile(smallFileName, secTimeout, t)
	validateFileResponse("", "", smallFileData, resp2, userlib.SUCCESSCODE, t)
	validateCacheSize(2, len(largeFileData)+len(smallFileData), t)
	resp3 := requestFile(anotherLargeFileName, secTimeout, t)
	validateFileResponse("", "", anotherLargeFileData, resp3, userlib.SUCCESSCODE, t)
	validateCacheNotExceeded(t)
	validateNumberOfReads(3, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestExceedCapacityManySmallToSmallerThanTotSizeThenExceededByMany(t *testing.T) {
	largeFileData := []byte("I am very many bytes of data!")
	smallFileData := []byte("IM SMALL")
	totFiles := 100
	secCap := len(smallFileData)*totFiles + 5
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	largeFileName := "/largefile.61c"
	smallFileName := "/smallfile.61c"
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		if filename == "."+largeFileName {
			data = largeFileData
		} else if filename[:len(smallFileName)+1] == "."+smallFileName {
			data = smallFileData
		} else {
			data = []byte("BAD NAME")
		}
		return
	})
	for i := 0; i < totFiles; i++ {
		resp2 := requestFile(smallFileName+fmt.Sprintf("%v", i), secTimeout, t)
		validateFileResponse("", "", smallFileData, resp2, userlib.SUCCESSCODE, t)
		validateCacheSize(i+1, len(smallFileData)*(i+1), t)
	}
	resp := requestFile(largeFileName, secTimeout, t)
	validateFileResponse("", "", largeFileData, resp, userlib.SUCCESSCODE, t)
	validateCacheNotExceeded(t)
	validateNumberOfReads(uint64(totFiles+1), reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestExceedCapacityManySmallToSmallerThanTotSizeThenExceededByOne(t *testing.T) {
	smallFileData := []byte("IM SMALL")
	largeFileData := []byte("123456")
	totFiles := 100
	secCap := len(smallFileData)*totFiles + 5
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	largeFileName := "/largefile.61c"
	smallFileName := "/smallfile.61c"
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		if filename == "."+largeFileName {
			data = largeFileData
		} else if filename[:len(smallFileName)+1] == "."+smallFileName {
			data = smallFileData
		} else {
			data = []byte("BAD NAME")
		}
		return
	})
	for i := 0; i < totFiles; i++ {
		resp2 := requestFile(smallFileName+fmt.Sprintf("%v", i), secTimeout, t)
		validateFileResponse("", "", smallFileData, resp2, userlib.SUCCESSCODE, t)
		validateCacheSize(i+1, len(smallFileData)*(i+1), t)
	}
	resp := requestFile(largeFileName, secTimeout, t)
	validateFileResponse("", "", largeFileData, resp, userlib.SUCCESSCODE, t)
	validateCacheNotExceeded(t)
	validateNumberOfReads(uint64(totFiles+1), reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestExceedCapacityManySmallAndOneBigExact(t *testing.T) {
	smallFileData := []byte("IM SMALL")
	totFiles := 100
	secCap := len(smallFileData)*totFiles + 2
	secTimeout := 2
	capacity = secCap
	largeFileData := []byte(strings.Repeat("a", capacity))
	timeout = secTimeout
	workingDir = ""
	launchCache()
	largeFileName := "/largefile.61c"
	smallFileName := "/smallfile.61c"
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		if filename == "."+largeFileName {
			data = largeFileData
		} else if filename[:len(smallFileName)+1] == "."+smallFileName {
			data = smallFileData
		} else {
			data = []byte("BAD NAME")
		}
		return
	})
	for i := 0; i < totFiles; i++ {
		resp2 := requestFile(smallFileName+fmt.Sprintf("%v", i), secTimeout, t)
		validateFileResponse("", "", smallFileData, resp2, userlib.SUCCESSCODE, t)
		validateCacheSize(i+1, len(smallFileData)*(i+1), t)
	}
	resp := requestFile(largeFileName, secTimeout, t)
	validateFileResponse("", "", largeFileData, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(largeFileData), t)
	validateNumberOfReads(uint64(totFiles+1), reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestExceedCapacityInsertFileLargerThanCache(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	largeFileData := []byte("I am very many bytes of data!")
	secCap := len(largeFileData) - 5
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	largeFileName := "/largefile.61c"
	anotherLargeFileName := "/anotherlargefile.61c"
	anotherLargeFileData := []byte("1sdfgsdfgs")
	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		if filename == "."+largeFileName {
			data = largeFileData
		} else if filename == "."+anotherLargeFileName {
			data = anotherLargeFileData
		} else {
			data = []byte("BAD NAME")
		}
		return
	})
	resp := requestFile(anotherLargeFileName, secTimeout, t)
	validateFileResponse("", "", anotherLargeFileData, resp, userlib.SUCCESSCODE, t)
	validateCacheSize(1, len(anotherLargeFileData), t)
	resp2 := requestFile(largeFileName, secTimeout, t)
	validateFileResponse("", "", largeFileData, resp2, userlib.SUCCESSCODE, t)
	validateCacheNotExceeded(t)
	resp3 := requestFile(largeFileName, secTimeout, t)
	validateFileResponse("", "", largeFileData, resp3, userlib.SUCCESSCODE, t)
	validateCacheNotExceeded(t)
	validateNumberOfReads(3, reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestExceedCapacityManyDifferentSizedFilesCapacityCheck(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	secCap := 10
	secTimeout := 2
	capacity = secCap
	iterations := 0x61c
	numFiles := 100
	timeout = secTimeout
	workingDir = ""
	launchCache()
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		data = []byte(fmt.Sprintf("FID:%v", filename[2:]))
		return
	})
	for i := 0; i < iterations; i++ {
		if i%2 == 0 {
			for i := numFiles - 1; i >= 0; i-- {
				fid := fmt.Sprintf("%v", i*100)
				resp := requestFile("/"+fid, secTimeout, t)
				if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
					t.FailNow()
				}
				if validateCacheNotExceeded(t) == true {
					t.FailNow()
				}
			}
		} else {
			for i := numFiles - 1; i >= 0; i-- {
				fid := fmt.Sprintf("%v", i*100)
				resp := requestFile("/"+fid, secTimeout, t)
				if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
					t.FailNow()
				}
				if validateCacheNotExceeded(t) == true {
					t.FailNow()
				}
			}
		}
		if i%5 == 0 {
			for i := (numFiles - 1) / 2; i >= 0; i-- {
				fid := fmt.Sprintf("%v", i*100)
				resp := requestFile("/"+fid, secTimeout, t)
				if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
					t.FailNow()
				}
				if validateCacheNotExceeded(t) == true {
					t.FailNow()
				}
			}
			for i := 0; i < numFiles/2; i++ {
				fid := fmt.Sprintf("%v", i*10000000000000)
				resp := requestFile("/"+fid, secTimeout, t)
				if validateFileResponse("", "", []byte("FID:"+fid), resp, userlib.SUCCESSCODE, t) == true {
					t.FailNow()
				}
				if validateCacheNotExceeded(t) == true {
					t.FailNow()
				}
			}
		}
		if capacity != secCap {
			t.Errorf("The max capacity has been changed when it should not have been!")
			os.Exit(61)
		}
		if timeout != secTimeout {
			t.Errorf("The timeout has been changed when it should not have been!")
			os.Exit(62)
		}
		if validateCacheNotExceeded(t) == true {
			t.FailNow()
		}
	}
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

// ============ End of Exceed Capacity Tests ============

// ============ Multithreading Tests ============
func TestMultithreadingTwoThreadsDifFiles(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	iterations := 1000
	secCap := 100
	secTimeout := 2
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	file1Name := "/thread1file.61c"
	file1Data := []byte("I am some spicy data for a good thread")
	file2Name := "/thread2file.61c"
	file2Data := []byte("I am some salty data for a good thread")

	var reads uint64 = 0
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		// Increment the read counter
		atomic.AddUint64(&reads, 1)
		if filename == "."+file1Name {
			time.Sleep(time.Duration(10) * time.Millisecond)
			data = file1Data
		} else if filename == "."+file2Name {
			data = file2Data
		} else {
			data = []byte("Invalid name!")
		}
		return
	})
	count := 0
	for j := 0; j < iterations; j++ {
		done := make(chan int)
		go func() {
			// Thread 1
			resp := requestFile(file1Name, secTimeout, t)
			if validateFileResponse("", "", file1Data, resp, userlib.SUCCESSCODE, t) == true {
				t.FailNow()
			}
			done <- 1
		}()
		go func() {
			// Thread 2
			resp2 := requestFile(file2Name, secTimeout, t)
			if validateFileResponse("", "", file2Data, resp2, userlib.SUCCESSCODE, t) == true {
				t.FailNow()
			}
			done <- 2
		}()
		for i := 2; i > 0; i-- {
			select {
			case id := <-done:
				if i == 2 && id == 1 {
					count++
				}
			}
		}
		if capacity != secCap {
			t.Errorf("The max capacity has been changed when it should not have been!")
			os.Exit(61)
		}
		if timeout != secTimeout {
			t.Errorf("The timeout has been changed when it should not have been!")
			os.Exit(62)
		}
		clearCache()
		if validateCacheSize(0, 0, t) == true {
			t.FailNow()
		}
	}
	if count >= ((iterations * 1) / 100) {
		t.Errorf("Thread 2 should have completed first a majority of the time. Completed first %v times out of %v.", count, iterations)
	}
	validateCacheNotExceeded(t)
	validateNumberOfReads(uint64(iterations*2), reads, t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}
func TestMultithreadingTwoThreadsSameFile(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	iterations := 100
	fileData := []byte("I am some spicy data for a good thread")
	secCap := (len(fileData)+3)*2 + 5
	secTimeout := 10
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	fileName := "/threadfile.61c_"
	readAllowed := true
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		if !readAllowed {
			t.Errorf("A read occured when the value should have been cached!")
		}
		if filename[:len(fileName)+1] == "."+fileName {
			time.Sleep(time.Duration(150) * time.Millisecond)
			data = []byte(string(fileData) + filename[len(fileName):])
		} else {
			data = []byte("Invalid name!")
		}
		return
	})
	fin := make(chan int)
	f := func(id int) {
		// Thread 1
		readAllowed = true
		resp := requestFile(fileName+fmt.Sprintf("%v", id), secTimeout, t)
		if validateFileResponse("", "", []byte(string(fileData)+fmt.Sprintf("_%v", id)), resp, userlib.SUCCESSCODE, t) == true {
			t.FailNow()
		}
		fin <- 1
	}
	go f(-1)
	go f(-1)
	for i := 2; i > 0; i-- {
		select {
		case <-fin:
		}
	}
	validateCacheSize(1, len(fileData)+3, t) // Three extra things: "_-1"
	for j := 0; j < iterations; j++ {
		done := make(chan int)
		f := func(id int) {
			// Thread 1
			readAllowed = true
			resp := requestFile(fileName+fmt.Sprintf("%v", id), secTimeout, t)
			if validateFileResponse("", "", []byte(string(fileData)+fmt.Sprintf("_%v", id)), resp, userlib.SUCCESSCODE, t) == true {
				t.FailNow()
			}
			done <- 1
		}
		go f(j)
		go f(j)
		for i := 2; i > 0; i-- {
			select {
			case <-done:
			}
		}
		if validateCacheNotExceeded(t) == true {
			t.FailNow()
		}
		readAllowed = false
		resp := requestFile(fileName+fmt.Sprintf("%v", j), secTimeout, t)
		if validateFileResponse("", "", []byte(string(fileData)+fmt.Sprintf("_%v", j)), resp, userlib.SUCCESSCODE, t) == true {
			t.FailNow()
		}
	}
	validateCacheNotExceeded(t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestMultithreadingNThreadWithMultipleRequestsSmall(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	fileData := []byte("I am some very spicy data")
	iterations := 1000
	numThreads := 10
	secCap := (len(fileData)+2)*numThreads + 10
	secTimeout := 10
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	fileBaseName := "/thread.61c_"
	reads := uint64(0)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		atomic.AddUint64(&reads, 1)
		if filename[:len(fileBaseName)+1] == "."+fileBaseName {
			time.Sleep(time.Duration(2) * time.Second)
			data = []byte(string(fileData) + filename[len(fileBaseName):])
		} else {
			data = []byte("Invalid name!")
		}
		return
	})
	for j := 0; j < iterations; j++ {
		done := make(chan int)
		for i := 0; i < numThreads; i++ {
			go func(id int) {
				// Thread i
				fid := fmt.Sprintf("%v", (id+iterations)%numThreads)
				resp := requestFile(fileBaseName+fid, secTimeout, t)
				if validateFileResponse("", "", []byte(string(fileData)+"_"+fid), resp, userlib.SUCCESSCODE, t) == true {
					t.FailNow()
				}
				if validateCacheNotExceeded(t) == true {
					t.FailNow()
				}
				done <- id
			}(i)
		}
		for i := numThreads; i > 0; i-- {
			select {
			case <-done:
			}
		}
		if validateCacheNotExceeded(t) == true {
			t.FailNow()
		}
	}
	validateNumberOfReads(uint64(numThreads), reads, t)
	validateCacheNotExceeded(t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

func TestMultithreadingNThreadWithMultipleRequestsLarge(t *testing.T) {
	// We first need to define a capacity and timeout so our cache has some parameters.
	fileData := []byte("I am some very spicy data")
	iterations := 1000
	numThreads := 1000
	secCap := (len(fileData)+4)*numThreads + 10
	secTimeout := 10
	capacity = secCap
	timeout = secTimeout
	workingDir = ""
	launchCache()
	fileBaseName := "/thread.61c_"
	reads := uint64(0)
	// We set the userlib FileRead function to this custom 'read'.
	userlib.ReplaceReadFile(func(workingDir, filename string) (data []byte, err error) {
		atomic.AddUint64(&reads, 1)
		if filename[:len(fileBaseName)+1] == "."+fileBaseName {
			time.Sleep(time.Duration(2) * time.Second)
			data = []byte(string(fileData) + filename[len(fileBaseName):])
		} else {
			data = []byte("Invalid name!")
		}
		return
	})
	for j := 0; j < iterations; j++ {
		done := make(chan int)
		for i := 0; i < numThreads; i++ {
			go func(id int) {
				// Thread i
				fid := fmt.Sprintf("%v", (id+iterations)%numThreads)
				resp := requestFile(fileBaseName+fid, secTimeout, t)
				if validateFileResponse("", "", []byte(string(fileData)+"_"+fid), resp, userlib.SUCCESSCODE, t) == true {
					t.FailNow()
				}
				if validateCacheNotExceeded(t) == true {
					t.FailNow()
				}
				done <- id
			}(i)
		}
		for i := numThreads; i > 0; i-- {
			select {
			case <-done:
			}
		}
		if validateCacheNotExceeded(t) == true {
			t.FailNow()
		}
	}
	validateNumberOfReads(uint64(numThreads), reads, t)
	validateCacheNotExceeded(t)
	if capacity != secCap {
		t.Errorf("The max capacity has been changed when it should not have been!")
		os.Exit(61)
	}
	if timeout != secTimeout {
		t.Errorf("The timeout has been changed when it should not have been!")
		os.Exit(62)
	}
	clearCache()
}

// ============ End of Multithreading Tests ============
