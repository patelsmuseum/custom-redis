package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type KeyValue struct {
	value  string
	expiry time.Time
}

var SETs = make(map[string]KeyValue)
var SETsMu = sync.RWMutex{}

var Handlers = map[string]func([]Value) Value{
	"PING":   ping,
	"ECHO":   echo,
	"SET":    set,
	"GET":    get,
	"CONFIG": config,
	"SAVE":   save,
	"KEYS":   keys,
}

func ping(args []Value) Value {
	if len(args) == 0 {
		return Value{typ: "string", str: "PONG"}
	}

	return Value{typ: "string", str: args[0].bulk}
}

func echo(args []Value) Value {
	if len(args) < 1 {
		return Value{typ: "string", str: "ERR wrong number of arguments for 'echo' command"}
	}
	return Value{typ: "string", str: args[0].bulk}
}

func set(args []Value) Value {
	if len(args) < 2 {
		return Value{typ: "error", str: "ERR wrong number of arguments for 'set' command"}
	}

	key := args[0].bulk
	value := args[1].bulk
	var exKey string
	exValue := time.Time{}
	if len(args) > 2 {
		exKey = args[2].bulk
		v, _ := strconv.Atoi(args[3].bulk)

		if exKey == "px" {
			exValue = time.Now().Add(time.Duration(v) * time.Millisecond)
		}

		SETsMu.Lock()
		SETs[key] = KeyValue{
			value: value,
		}
		SETsMu.Unlock()
	}

	SETsMu.Lock()
	SETs[key] = KeyValue{
		value:  value,
		expiry: exValue,
	}
	SETsMu.Unlock()

	return Value{typ: "string", str: "OK"}
}

func get(args []Value) Value {
	if len(args) < 1 {
		return Value{typ: "error", str: "ERR wrong number of arguments for 'get' command"}
	}
	key := args[0].bulk
	fmt.Println("pattern: ", key)
	filePath := RDBMap["dir"] + "/" + RDBMap["dbfilename"]

	// If the file doesn't exist, try writing the current in‑memory data to disk.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Println("RDB file not found; calling initRDB to create it")
		initRDB()
	}

	data, err := readRDBFile(filePath)
	if err != nil {
		fmt.Println("error reading file file not found...", err)
		return Value{typ: "error", str: "ERR unable to read file"}
	}
	fmt.Println("data: ", data)

	// Check if the key exists
	kv, exists := data[key]
	if !exists || kv.value == "" {
		// check in memory
		fmt.Println("key not found in file, checking in memory")
		val := SETs[key]
		if val.value == "" {
			return Value{typ: "null"}
		}
		if val.expiry != (time.Time{}) && time.Now().After(val.expiry) {
			SETsMu.Lock()
			delete(SETs, key)
			SETsMu.Unlock()
			return Value{typ: "null"}
		}
		fmt.Println("value: ", val.value)
		return Value{typ: "bulk", bulk: val.value}
	}
	fmt.Println("value: ", kv.value)
	return Value{typ: "bulk", bulk: kv.value}
}

func config(args []Value) Value {
	if len(args) < 2 {
		return Value{typ: "error", str: "ERR wrong number of arguments for 'config' command"}
	}
	if strings.ToUpper(args[0].bulk) == "GET" {
		if args[1].bulk == "dir" {
			return getDirConfig()
		} else if args[1].bulk == "dbfilename" {
			return getDBFilenameConfig()
		} else {
			return Value{typ: "error", str: "ERR invalid parameters for 'config' command"}
		}
	} else {
		return Value{typ: "error", str: "ERR invalid parameters for 'config' command"}
	}
}

func save(args []Value) Value {
	initRDB()
	return Value{typ: "string", str: "OK"}
}

func keys(args []Value) Value {
	if len(args) < 1 {
		return Value{typ: "error", str: "ERR wrong number of arguments for 'keys' command"}
	}
	pattern := args[0]
	data, err := readRDBFile(RDBMap["dir"] + "/" + RDBMap["dbfilename"])
	if err != nil {
		fmt.Println("error reading file...", err)
		return Value{typ: "error", str: "ERR unable to read file"}
	}
	var keys []Value
	for key, _ := range data {
		if pattern.bulk != "*" {
			if strings.Contains(key, pattern.bulk) {
				keys = append(keys, Value{typ: "string", str: key})
			}
		} else {
			keys = append(keys, Value{typ: "string", str: key})
		}
	}
	return Value{typ: "array", array: keys}
}
