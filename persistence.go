package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc64"
	"io"
	"os"
	"time"
)

var RDBMap = map[string]string{
	"dir":        ".",
	"dbfilename": "dump.rdb",
}

var crc64ECMATable = crc64.MakeTable(crc64.ECMA)

const (
	STRING_TYPE      = 0x00
	METADATA_SECTION = 0xFA
	DB_INDEX         = 0x00
	DB_SECTION       = 0xFE
	HASHTABLE_SIZE   = 0xFB
	KEYVALUE_EXPIRY  = 0xFC
	EOF              = 0xFF
)

// getDirConfig returns configuration for the directory.
func getDirConfig() Value {
	array := make([]Value, 2)
	array[0].typ = "bulk"
	array[0].bulk = "dir"
	array[1].typ = "bulk"
	array[1].bulk = RDBMap["dir"]
	return Value{typ: "array", array: array}
}

// getDBFilenameConfig returns the database filename configuration.
func getDBFilenameConfig() Value {
	array := make([]Value, 2)
	array[0].typ = "bulk"
	array[0].bulk = "dbfilename"
	array[1].typ = "bulk"
	array[1].bulk = RDBMap["dbfilename"]
	return Value{typ: "array", array: array}
}

// initRDB creates/overwrites the RDB file using the updated format.
func initRDB() {
	dir := RDBMap["dir"]
	dbfilename := RDBMap["dbfilename"]
	path := dir + "/" + dbfilename
	// Use TRUNC to overwrite an existing file.
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Println("error opening file:", err)
		return
	}
	defer file.Close()
	writer := bufio.NewWriter(file)

	// 1. Write header ("REDIS0011")
	writer.Write([]byte("REDIS0011"))

	// 2. Write metadata entry for redis-ver ("7.2.0")
	writer.Write([]byte{METADATA_SECTION})
	stringEncoding(writer, "redis-ver")
	stringEncoding(writer, "7.2.0")

	// 3. Write metadata entry for redis-bits with a 2-byte value (0xc0 0x40)
	writer.Write([]byte{METADATA_SECTION})
	stringEncoding(writer, "redis-bits")
	writer.Write([]byte{0xc0, 0x40})

	// 4. Write DB section header:
	//    • DB_SECTION marker
	//    • DB index (0x00)
	//    • Hashtable size marker (0xFB) followed by count (number of key–values)
	//    • Expiry count (0x00 as no key has expiry here)
	writer.Write([]byte{DB_SECTION})
	writer.Write([]byte{DB_INDEX})
	writer.Write([]byte{HASHTABLE_SIZE, byte(len(SETs))})
	writer.Write([]byte{0x00})

	// 5. Write key–value pairs.
	for key, value := range SETs {
		writeKeyValue(writer, key, value)
	}

	// 6. Write EOF marker.
	writer.Write([]byte{EOF})

	// 7. Flush and then calculate/write the checksum.
	writer.Flush()
	checksum, err := calculateCRC64Checksum(path)
	if err != nil {
		fmt.Println("error calculating checksum:", err)
		return
	}
	writer.Write(checksum)
	writer.Flush()
	fmt.Println("RDB file written successfully")
}

// writeKeyValue writes a single key–value pair with an optional expiry.
func writeKeyValue(writer *bufio.Writer, key string, value KeyValue) {
	if value.expiry.IsZero() {
		writer.Write([]byte{STRING_TYPE})
		stringEncoding(writer, key)
		stringEncoding(writer, value.value)
	} else {
		writer.Write([]byte{KEYVALUE_EXPIRY})
		timestampEncoding(writer, value.expiry)
		writer.Write([]byte{STRING_TYPE})
		stringEncoding(writer, key)
		stringEncoding(writer, value.value)
	}
}

// stringEncoding writes a length-encoded string.
func stringEncoding(writer *bufio.Writer, s string) error {
	bytes, err := lengthEncoding(len(s))
	if err != nil {
		fmt.Println(err)
		return err
	}
	writer.Write(bytes)
	writer.Write([]byte(s))
	return nil
}

// lengthEncoding returns a one- or two-byte representation for short strings.
func lengthEncoding(strLen int) ([]byte, error) {
	if strLen < (1 << 6) {
		return []byte{byte(strLen)}, nil
	}
	if strLen < (1 << 14) {
		return []byte{byte(strLen>>8) | 0x40, byte(strLen)}, nil
	}
	return nil, fmt.Errorf("length too large %d", strLen)
}

func timestampEncoding(w *bufio.Writer, expire time.Time) {
	buffer := make([]byte, 8)
	binary.LittleEndian.PutUint64(buffer, uint64(expire.UnixMilli()))
	w.Write(buffer)
}

// calculateCRC64Checksum computes the CRC64 checksum over the file.
func calculateCRC64Checksum(filepath string) ([]byte, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	hash := crc64.New(crc64ECMATable)
	buffer := make([]byte, 64*1024)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		hash.Write(buffer[:n])
	}
	checksum := hash.Sum64()
	checksumBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(checksumBytes, checksum)
	return checksumBytes, nil
}

// openRDBFile opens the file for reading.
func openRDBFile(filepath string) (*bufio.Reader, *os.File, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, nil, err
	}
	reader := bufio.NewReader(file)
	return reader, file, nil
}

// verifyHeader reads and checks the header.
func verifyHeader(reader *bufio.Reader) error {
	expectedHeader := []byte("REDIS0011")
	header := make([]byte, len(expectedHeader))
	_, err := io.ReadFull(reader, header)
	if err != nil {
		return err
	}
	fmt.Println("Header:", string(header))
	if string(header) != string(expectedHeader) {
		return fmt.Errorf("invalid RDB header")
	}
	return nil
}

// readMetadata reads metadata entries until the DB_SECTION marker is reached.
func readMetadata(reader *bufio.Reader) (map[string]interface{}, error) {
	metadata := make(map[string]interface{})
	for {
		// Peek to see if we have reached the DB_SECTION marker.
		b, err := reader.Peek(1)
		if err != nil {
			return nil, err
		}
		if b[0] == DB_SECTION {
			break
		}
		marker, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if marker != METADATA_SECTION {
			return nil, fmt.Errorf("expected metadata marker, got %x", marker)
		}
		// Read key.
		key, err := readString(reader)
		if err != nil {
			return nil, err
		}
		// For "redis-ver" we expect a string value.
		if key == "redis-ver" {
			val, err := readString(reader)
			if err != nil {
				return nil, err
			}
			metadata[key] = val
		} else if key == "redis-bits" {
			// For "redis-bits" read a 2-byte integer.
			buf := make([]byte, 2)
			_, err := io.ReadFull(reader, buf)
			if err != nil {
				return nil, err
			}
			metadata[key] = binary.LittleEndian.Uint16(buf)
		} else {
			// Fallback: try to read a string.
			val, err := readString(reader)
			if err != nil {
				return nil, err
			}
			metadata[key] = val
		}
	}
	return metadata, nil
}

// readLengthEncoded reads a length-encoded integer.
func readLengthEncoded(reader *bufio.Reader) (int, error) {
	firstByte, err := reader.ReadByte()
	if err != nil {
		return 0, err
	}
	if firstByte>>6 == 0 {
		return int(firstByte), nil
	}
	if firstByte>>6 == 1 {
		secondByte, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		length := (int(firstByte&0x3F) << 8) | int(secondByte)
		return length, nil
	}
	return 0, fmt.Errorf("unsupported length encoding")
}

// readString reads a string with a preceding length encoding.
func readString(reader *bufio.Reader) (string, error) {
	length, err := readLengthEncoded(reader)
	if err != nil {
		return "", err
	}
	data := make([]byte, length)
	_, err = io.ReadFull(reader, data)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readTimestamp(reader *bufio.Reader) (time.Time, error) {
	buffer := make([]byte, 8)
	_, err := io.ReadFull(reader, buffer)
	if err != nil {
		return time.Time{}, err
	}
	timestamp := binary.LittleEndian.Uint64(buffer)
	return time.UnixMilli(int64(timestamp)), nil
}

func readKeyValue(reader *bufio.Reader) (string, string, error) {
	typ, err := reader.ReadByte()
	if err != nil {
		return "", "", err
	}
	if typ == KEYVALUE_EXPIRY {
		expiry, err := readTimestamp(reader)
		if err != nil {
			return "", "", err
		}
		if time.Now().After(expiry) {
			// Key is expired: read and discard the key–value pair.
			typ, err = reader.ReadByte()
			if err != nil {
				return "", "", err
			}
			if typ != STRING_TYPE {
				return "", "", fmt.Errorf("unsupported type: %x", typ)
			}
			// Discard key and value.
			_, err = readString(reader)
			if err != nil {
				return "", "", err
			}
			_, err = readString(reader)
			if err != nil {
				return "", "", err
			}
			return "", "", nil
		}
		// Not expired; continue by reading the next type.
		typ, err = reader.ReadByte()
		if err != nil {
			return "", "", err
		}
	}
	if typ != STRING_TYPE {
		return "", "", fmt.Errorf("unsupported type: %x", typ)
	}
	key, err := readString(reader)
	if err != nil {
		return "", "", err
	}
	value, err := readString(reader)
	if err != nil {
		return "", "", err
	}
	return key, value, nil
}

// readRDBFile opens and parses the RDB file.
func readRDBFile(filepath string) (map[string]KeyValue, error) {
	reader, file, err := openRDBFile(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if fileInfo.Size() == 0 {
		return nil, fmt.Errorf("RDB file is empty")
	}

	if err := verifyHeader(reader); err != nil {
		return nil, err
	}

	_, err = readMetadata(reader)
	if err != nil {
		return nil, err
	}

	// Read DB section marker.
	b, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if b != DB_SECTION {
		return nil, fmt.Errorf("expected DB_SECTION marker, got %x", b)
	}

	// Read DB index, hashtable size marker and count, and expiry count.
	_, err = reader.ReadByte() // DB index
	if err != nil {
		return nil, err
	}
	hashtableMarker, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if hashtableMarker != HASHTABLE_SIZE {
		return nil, fmt.Errorf("expected hashtable size marker, got %x", hashtableMarker)
	}
	kvCount, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	_, err = reader.ReadByte() // expiry count
	if err != nil {
		return nil, err
	}

	keyValues := make(map[string]KeyValue)
	for range int(kvCount) {
		key, value, err := readKeyValue(reader)
		if err != nil {
			return nil, err
		}
		if key == "" {
			continue
		}
		keyValues[key] = KeyValue{value: value}
	}

	// Read EOF marker.
	eofMarker, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if eofMarker != EOF {
		return nil, fmt.Errorf("expected EOF marker, got %x", eofMarker)
	}

	//Read checksum (8 bytes).
	checksumBytes := make([]byte, 8)
	_, err = io.ReadFull(reader, checksumBytes)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Checksum read: %x\n", checksumBytes)

	return keyValues, nil
}
