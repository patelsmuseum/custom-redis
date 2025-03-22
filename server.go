package main

import (
	"flag"
	"fmt"
	"net"
	"os"
)

func main() {
	dir := flag.String("dir", ".", "")
	dbfilename := flag.String("dbfilename", "dump.rdb", "")
	flag.Parse()
	RDBMap["dir"] = *dir
	RDBMap["dbfilename"] = *dbfilename

	l, err := net.Listen("tcp", "0.0.0.0:6379")
	if err != nil {
		fmt.Println("Failed to bind to port 6379")
		os.Exit(1)
	}
	for {
		fmt.Println("server started listening on port: 6379")
		fmt.Println("ready to accept connections...")
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			os.Exit(1)
		}
		go handleConnection(conn)
	}
}
