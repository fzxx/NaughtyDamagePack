package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
)

const (
	zipLocalFileSig    = 0x04034b50
	zipCentralDirSig   = 0x02014b50
	zipEndOfCentralSig = 0x06054b50
	rarSignature       = 0x21726152
	sevenZSignature    = 0xAFBC7A37
)

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) < 1 {
		fmt.Println("使用方法: NaughtyDamagePack [压缩文件路径]")
		fmt.Println("支持格式: ZIP, RAR, 7z")
		return
	}

	filePath := args[0]

	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_SYNC, 0644)
	if err != nil {
		fmt.Printf("无法打开文件: %v\n", err)
		return
	}
	defer file.Close()

	fileType, err := detectFileType(file)
	if err != nil {
		fmt.Printf("无法识别文件类型: %v\n", err)
		return
	}

	fmt.Printf("检测到文件类型: %s\n", fileType)

	switch fileType {
	case "ZIP":
		err = processZIP(file)
	case "RAR":
		err = processRAR(file)
	case "7z":
		err = process7z(file)
	default:
		err = fmt.Errorf("不支持的文件格式")
	}

	if err != nil {
		fmt.Printf("处理失败: %v\n", err)
		return
	}

	fmt.Println("加密状态已成功切换，文件可正常使用")
}

func detectFileType(file *os.File) (string, error) {
	buf := make([]byte, 32)
	n, err := file.ReadAt(buf, 0)
	if err != nil {
		return "", fmt.Errorf("读取文件头失败: %v", err)
	}
	if n < 8 {
		return "", fmt.Errorf("文件头过短，需要至少8字节，实际读取%d字节", n)
	}

	zipSig := binary.LittleEndian.Uint32(buf[0:4])
	if zipSig == zipLocalFileSig || zipSig == zipCentralDirSig || zipSig == zipEndOfCentralSig {
		return "ZIP", nil
	}

	rarSig := binary.LittleEndian.Uint32(buf[0:4])
	if rarSig == rarSignature {
		if n >= 8 {
			if buf[4] == 0x1A && buf[5] == 0x07 && buf[6] == 0x00 {
				return "RAR", nil
			}
			if buf[4] == 0x1A && buf[5] == 0x07 && buf[6] == 0x01 {
				return "RAR", nil
			}
		}
	}

	if n >= 6 {
		sevenZSig := binary.LittleEndian.Uint32(buf[0:4])
		if sevenZSig == sevenZSignature {
			if buf[4] == 0x27 && buf[5] == 0x1C {
				return "7z", nil
			}
		}
	}

	return "", fmt.Errorf("未知的压缩文件格式")
}

func processZIP(file *os.File) error {
	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}
	fileSize := fileInfo.Size()

	eocd, err := findEndOfCentralDir(file, fileSize)
	if err != nil {
		return err
	}

	isEncrypted, err := checkZIPEncryptionStatus(file, eocd)
	if err != nil {
		return err
	}

	fmt.Printf("当前状态: %s\n", map[bool]string{true: "加密", false: "未加密"}[isEncrypted])
	newEncrypted := !isEncrypted

	return toggleZIPEncryptionFlags(file, eocd, newEncrypted)
}

func processRAR(file *os.File) error {
	header := make([]byte, 32)
	n, err := file.ReadAt(header, 0)
	if err != nil || n < len(header) {
		return fmt.Errorf("读取RAR头部失败: %v", err)
	}

	byte1Offset := 8
	byte2Offset := 9

	byte1 := header[byte1Offset]
	byte2 := header[byte2Offset]
	isModified := byte1 > byte2

	fmt.Printf("当前状态: %s (字节: 0x%02X 0x%02X)\n",
		map[bool]string{true: "已破坏", false: "正常"}[isModified], byte1, byte2)
	if isModified {
		header[byte1Offset] = byte2
		header[byte2Offset] = byte1
	} else {
		header[byte1Offset] = byte2
		header[byte2Offset] = byte1
	}
	_, err = file.WriteAt([]byte{header[byte1Offset], header[byte2Offset]}, int64(byte1Offset))
	return err
}

func process7z(file *os.File) error {
	header := make([]byte, 16)
	n, err := file.ReadAt(header, 0)
	if err != nil || n < len(header) {
		return fmt.Errorf("读取7z头部失败: %v", err)
	}
	byte1Offset := 8
	byte2Offset := 9

	byte1 := header[byte1Offset]
	byte2 := header[byte2Offset]
	isModified := byte1 > byte2

	fmt.Printf("当前状态: %s (字节: 0x%02X 0x%02X)\n",
		map[bool]string{true: "已破坏", false: "正常"}[isModified], byte1, byte2)
	if isModified {
		header[byte1Offset] = byte2
		header[byte2Offset] = byte1
	} else {
		header[byte1Offset] = byte2
		header[byte2Offset] = byte1
	}
	_, err = file.WriteAt([]byte{header[byte1Offset], header[byte2Offset]}, int64(byte1Offset))
	return err
}

type endOfCentralDir struct {
	Signature       uint32
	DiskNum         uint16
	StartDisk       uint16
	EntriesThisDisk uint16
	TotalEntries    uint16
	DirSize         uint32
	DirOffset       uint32
	CommentLen      uint16
}

type centralDirHeader struct {
	Signature    uint32
	VersionMade  uint16
	VersionNeed  uint16
	Flags        uint16
	Method       uint16
	ModTime      uint16
	ModDate      uint16
	Crc32        uint32
	CompSize     uint32
	UncompSize   uint32
	FilenameLen  uint16
	ExtraLen     uint16
	CommentLen   uint16
	DiskNumStart uint16
	Attrs        uint16
	ExtAttrs     uint32
	LocalOffset  uint32
}

func findEndOfCentralDir(file *os.File, fileSize int64) (*endOfCentralDir, error) {
	searchStart := fileSize - 65535 - 22
	if searchStart < 0 {
		searchStart = 0
	}

	buf := make([]byte, fileSize-searchStart)
	_, err := file.ReadAt(buf, searchStart)
	if err != nil {
		return nil, err
	}

	for i := len(buf) - 22; i >= 0; i-- {
		sig := binary.LittleEndian.Uint32(buf[i:])
		if sig == zipEndOfCentralSig {
			eocd := &endOfCentralDir{
				Signature:       sig,
				DiskNum:         binary.LittleEndian.Uint16(buf[i+4:]),
				StartDisk:       binary.LittleEndian.Uint16(buf[i+6:]),
				EntriesThisDisk: binary.LittleEndian.Uint16(buf[i+8:]),
				TotalEntries:    binary.LittleEndian.Uint16(buf[i+10:]),
				DirSize:         binary.LittleEndian.Uint32(buf[i+12:]),
				DirOffset:       binary.LittleEndian.Uint32(buf[i+16:]),
				CommentLen:      binary.LittleEndian.Uint16(buf[i+20:]),
			}
			return eocd, nil
		}
	}

	return nil, fmt.Errorf("未找到ZIP中央目录结束记录")
}

func checkZIPEncryptionStatus(file *os.File, eocd *endOfCentralDir) (bool, error) {
	var firstHeader centralDirHeader
	err := readCentralDirHeader(file, int64(eocd.DirOffset), &firstHeader)
	if err != nil {
		return false, err
	}
	return (firstHeader.Flags & 0x0001) != 0, nil
}

func toggleZIPEncryptionFlags(file *os.File, eocd *endOfCentralDir, newEncrypted bool) error {
	offset := int64(eocd.DirOffset)
	totalEntries := int(eocd.TotalEntries)
	processed := 0

	for processed < totalEntries && offset < int64(eocd.DirOffset+eocd.DirSize) {
		var header centralDirHeader
		if err := readCentralDirHeader(file, offset, &header); err != nil {
			return err
		}

		if header.Signature != zipCentralDirSig {
			return fmt.Errorf("无效的ZIP中央目录项")
		}

		newFlags := header.Flags
		if newEncrypted {
			newFlags |= 0x0001
		} else {
			newFlags &^= 0x0001
		}

		if err := writeUint16(file, offset+8, newFlags); err != nil {
			return err
		}

		localHeaderOffset := int64(header.LocalOffset)
		localFlags, err := readUint16(file, localHeaderOffset+6)
		if err != nil {
			return err
		}

		newLocalFlags := localFlags
		if newEncrypted {
			newLocalFlags |= 0x0001
		} else {
			newLocalFlags &^= 0x0001
		}

		if err := writeUint16(file, localHeaderOffset+6, newLocalFlags); err != nil {
			return err
		}

		offset += 46 + int64(header.FilenameLen) + int64(header.ExtraLen) + int64(header.CommentLen)
		processed++
	}

	return nil
}

func readCentralDirHeader(file *os.File, offset int64, header *centralDirHeader) error {
	sig, err := readUint32(file, offset)
	if err != nil {
		return err
	}
	header.Signature = sig

	header.VersionMade, err = readUint16(file, offset+4)
	if err != nil {
		return err
	}

	header.VersionNeed, err = readUint16(file, offset+6)
	if err != nil {
		return err
	}

	header.Flags, err = readUint16(file, offset+8)
	if err != nil {
		return err
	}

	header.Method, err = readUint16(file, offset+10)
	if err != nil {
		return err
	}

	header.ModTime, err = readUint16(file, offset+12)
	if err != nil {
		return err
	}

	header.ModDate, err = readUint16(file, offset+14)
	if err != nil {
		return err
	}

	header.Crc32, err = readUint32(file, offset+16)
	if err != nil {
		return err
	}

	header.CompSize, err = readUint32(file, offset+20)
	if err != nil {
		return err
	}

	header.UncompSize, err = readUint32(file, offset+24)
	if err != nil {
		return err
	}

	header.FilenameLen, err = readUint16(file, offset+28)
	if err != nil {
		return err
	}

	header.ExtraLen, err = readUint16(file, offset+30)
	if err != nil {
		return err
	}

	header.CommentLen, err = readUint16(file, offset+32)
	if err != nil {
		return err
	}

	header.DiskNumStart, err = readUint16(file, offset+34)
	if err != nil {
		return err
	}

	header.Attrs, err = readUint16(file, offset+36)
	if err != nil {
		return err
	}

	header.ExtAttrs, err = readUint32(file, offset+38)
	if err != nil {
		return err
	}

	header.LocalOffset, err = readUint32(file, offset+42)
	if err != nil {
		return err
	}

	return nil
}

func readUint16(file *os.File, offset int64) (uint16, error) {
	b := make([]byte, 2)
	_, err := file.ReadAt(b, offset)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b), nil
}

func readUint32(file *os.File, offset int64) (uint32, error) {
	b := make([]byte, 4)
	_, err := file.ReadAt(b, offset)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func writeUint16(file *os.File, offset int64, value uint16) error {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, value)
	_, err := file.WriteAt(b, offset)
	return err
}
