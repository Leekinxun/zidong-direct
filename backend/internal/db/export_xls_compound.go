package db

import (
	"bytes"
	"io"
	"unicode/utf16"
)

const (
	xlsSectorSize = 512
	xlsMiniCutoff = 4096
	xlsFreeSector = 0xffffffff
	xlsChainEnd   = 0xfffffffe
	xlsFATSector  = 0xfffffffd
	xlsDIFATSect  = 0xfffffffc
)

func writeXLSCompound(w io.Writer, workbookParts ...[]byte) error {
	workbookSize := 0
	for _, part := range workbookParts {
		workbookSize += len(part)
	}
	if workbookSize > xlsMaxBIFF {
		return errXLSSize
	}
	// Keep the Workbook stream in regular sectors, including small sheets,
	// by padding it to the compound-file mini stream cutoff.
	streamSize := workbookSize
	if streamSize < xlsMiniCutoff {
		streamSize = xlsMiniCutoff
	}
	workbookSectors := xlsCeilDiv(streamSize, xlsSectorSize)
	fatSectors, difatSectors := xlsAllocation(workbookSectors)
	fatStart := 1 + workbookSectors
	difatStart := fatStart + fatSectors

	header := xlsCompoundHeader(fatSectors, difatSectors, fatStart, difatStart)
	var directory [xlsSectorSize]byte
	xlsDirectoryEntry(directory[:128], "Root Entry", 5, 1, xlsChainEnd, 0)
	xlsDirectoryEntry(directory[128:256], "Workbook", 2, xlsFreeSector, 1, uint64(streamSize))
	if err := xlsWrite(w, header[:]); err != nil {
		return err
	}
	if err := xlsWrite(w, directory[:]); err != nil {
		return err
	}
	for _, part := range workbookParts {
		if err := xlsWrite(w, part); err != nil {
			return err
		}
	}
	padding := make([]byte, workbookSectors*xlsSectorSize-workbookSize)
	if err := xlsWrite(w, padding); err != nil {
		return err
	}

	fat := bytes.Repeat([]byte{0xff}, fatSectors*xlsSectorSize)
	xlsOrder.PutUint32(fat, xlsChainEnd) // Directory sector.
	for sector := 1; sector < fatStart-1; sector++ {
		xlsOrder.PutUint32(fat[4*sector:], uint32(sector+1))
	}
	xlsOrder.PutUint32(fat[4*(fatStart-1):], xlsChainEnd)
	for sector := fatStart; sector < difatStart; sector++ {
		xlsOrder.PutUint32(fat[4*sector:], xlsFATSector)
	}
	for sector := difatStart; sector < difatStart+difatSectors; sector++ {
		xlsOrder.PutUint32(fat[4*sector:], xlsDIFATSect)
	}
	if err := xlsWrite(w, fat); err != nil {
		return err
	}
	for i := 0; i < difatSectors; i++ {
		data := bytes.Repeat([]byte{0xff}, xlsSectorSize)
		for j := 0; j < 127; j++ {
			index := 109 + i*127 + j
			if index >= fatSectors {
				break
			}
			xlsOrder.PutUint32(data[4*j:], uint32(fatStart+index))
		}
		next := uint32(xlsChainEnd)
		if i+1 < difatSectors {
			next = uint32(difatStart + i + 1)
		}
		xlsOrder.PutUint32(data[508:], next)
		if err := xlsWrite(w, data); err != nil {
			return err
		}
	}
	return nil
}

func xlsAllocation(workbookSectors int) (fat, difat int) {
	fat = xlsCeilDiv(1+workbookSectors, 128)
	for {
		if fat > 109 {
			difat = xlsCeilDiv(fat-109, 127)
		}
		needed := xlsCeilDiv(1+workbookSectors+fat+difat, 128)
		if needed == fat {
			return fat, difat
		}
		fat = needed
	}
}

func xlsCompoundHeader(fatSectors, difatSectors, fatStart, difatStart int) [xlsSectorSize]byte {
	var header [xlsSectorSize]byte
	copy(header[:], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1})
	xlsOrder.PutUint16(header[24:], 0x003e) // Minor version
	xlsOrder.PutUint16(header[26:], 3)
	xlsOrder.PutUint16(header[28:], 0xfffe) // Little-endian
	xlsOrder.PutUint16(header[30:], 9)      // 512-byte sectors
	xlsOrder.PutUint16(header[32:], 6)      // 64-byte mini sectors
	xlsOrder.PutUint32(header[44:], uint32(fatSectors))
	xlsOrder.PutUint32(header[48:], 0) // Directory sector index.
	xlsOrder.PutUint32(header[56:], xlsMiniCutoff)
	xlsOrder.PutUint32(header[60:], xlsChainEnd)
	xlsOrder.PutUint32(header[68:], xlsChainEnd)
	if difatSectors > 0 {
		xlsOrder.PutUint32(header[68:], uint32(difatStart))
		xlsOrder.PutUint32(header[72:], uint32(difatSectors))
	}
	for i := 0; i < 109; i++ {
		sector := uint32(xlsFreeSector)
		if i < fatSectors {
			sector = uint32(fatStart + i)
		}
		xlsOrder.PutUint32(header[76+4*i:], sector)
	}
	return header
}

func xlsDirectoryEntry(entry []byte, name string, kind byte, child, sector uint32, size uint64) {
	units := utf16.Encode([]rune(name))
	for i, unit := range units {
		xlsOrder.PutUint16(entry[2*i:], unit)
	}
	xlsOrder.PutUint16(entry[64:], uint16((len(units)+1)*2))
	entry[66], entry[67] = kind, 1 // Object type and black directory node.
	xlsOrder.PutUint32(entry[68:], xlsFreeSector)
	xlsOrder.PutUint32(entry[72:], xlsFreeSector)
	xlsOrder.PutUint32(entry[76:], child)
	xlsOrder.PutUint32(entry[116:], sector)
	xlsOrder.PutUint64(entry[120:], size)
}

func xlsCeilDiv(value, unit int) int {
	return (value + unit - 1) / unit
}

func xlsWrite(w io.Writer, data []byte) error {
	if n, err := w.Write(data); err != nil {
		return err
	} else if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
