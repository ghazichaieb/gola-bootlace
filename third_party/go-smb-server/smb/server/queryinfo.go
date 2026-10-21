package server

import (
	"context"

	"github.com/sonroyaalmerol/go-smb-server/smb/vfs"
	"github.com/sonroyaalmerol/go-smb-server/smb/wire"
)

func (c *conn) handleQueryInfo(ctx context.Context, msg []byte, tr *tree) uint32 {
	var req wire.QueryInfoRequest
	if err := req.Parse(msg); err != nil {
		c.log.Debug("query info request cannot be read", "err", err, "bytes", len(msg))
		return c.errBody(wire.StatusInvalidParameter)
	}
	c.log.Debug("query info", "type", req.InfoType, "class", req.FileInfoClass)
	oh, ok := tr.opens[req.FileId]
	if !ok {
		return c.errBody(wire.StatusInvalidHandle)
	}

	switch req.InfoType {
	case wire.InfoFile:
		fi, err := oh.h.Stat(ctx)
		if err != nil {
			return c.errBody(osErrToStatus(err))
		}
		basic := wire.FileBasicInformation{
			CreationTime:   wire.TimeToFiletime(fi.CreationTime),
			LastAccessTime: wire.TimeToFiletime(fi.LastAccess),
			LastWriteTime:  wire.TimeToFiletime(fi.LastWrite),
			ChangeTime:     wire.TimeToFiletime(fi.ChangeTime),
			FileAttributes: toFileAttributes(fi),
		}
		standard := wire.FileStandardInformation{
			AllocationSize: uint64(fi.Size),
			EndOfFile:      uint64(fi.Size),
			NumberOfLinks:  1,
			DeletePending:  boolToU8(oh.deletePending),
			Directory:      boolToU8(fi.IsDir),
		}
		var info []byte
		switch req.FileInfoClass {
		case wire.FileBasicInfoClass:
			info = basic.Append(nil)
		case wire.FileStandardInfoClass:
			info = standard.Append(nil)
		case wire.FileAllInformation:
			info = wire.FileAllInformationAppend(nil, basic, standard)
		case wire.FileNetworkOpenInformation:
			info = networkOpenInfo(basic, fi.Size)
		case 0x06: // FileInternalInformation: a file number
			info = make([]byte, 8)
			info[0] = 1
		case 0x07: // FileEaInformation: no extended attributes
			info = make([]byte, 4)
		case 0x23: // FileAttributeTagInformation: attributes and a reparse tag (none)
			info = make([]byte, 8)
			info[0], info[1], info[2], info[3] = byte(basic.FileAttributes), byte(basic.FileAttributes>>8), byte(basic.FileAttributes>>16), byte(basic.FileAttributes>>24)
		default:
			c.log.Debug("file information class not supported", "class", req.FileInfoClass)
			return c.errBody(wire.StatusInvalidParameter)
		}
		if uint32(len(info)) > req.OutputBufferLength {
			info = info[:req.OutputBufferLength]
		}
		c.out = wire.QueryInfoResponseAppend(c.out, info)
		return wire.StatusSuccess

	case wire.InfoFilesystem:
		info := c.filesystemInfo(req.FileInfoClass)
		if info == nil {
			c.log.Debug("file system information class not supported", "class", req.FileInfoClass)
			return c.errBody(wire.StatusInvalidParameter)
		}
		if uint32(len(info)) > req.OutputBufferLength {
			info = info[:req.OutputBufferLength]
		}
		c.out = wire.QueryInfoResponseAppend(c.out, info)
		return wire.StatusSuccess

	default:
		return c.errBody(wire.StatusNotSupported)
	}
}

func networkOpenInfo(basic wire.FileBasicInformation, size int64) []byte {
	out := make([]byte, 56)
	putLE64(out[0:8], basic.CreationTime)
	putLE64(out[8:16], basic.LastAccessTime)
	putLE64(out[16:24], basic.LastWriteTime)
	putLE64(out[24:32], basic.ChangeTime)
	putLE64(out[32:40], uint64(size))
	putLE64(out[40:48], uint64(size))
	out[48], out[49], out[50], out[51] = byte(basic.FileAttributes), byte(basic.FileAttributes>>8), byte(basic.FileAttributes>>16), byte(basic.FileAttributes>>24)
	return out
}

func (c *conn) filesystemInfo(class uint8) []byte {
	switch class {
	case wire.FileFsAttributeInformation:
		// FileFsAttributeInformation: attributes (case sensitive search, case preserved, unicode, read-only volume), the longest name a part of a path
		// may have, and the name of the file system. The library once put the length of the file system name in the second field, so every name longer
		// than six characters was refused by Windows.
		name := wire.UTF16ToBytes("NTFS")
		out := make([]byte, 12+len(name))
		out[0], out[1], out[2] = 0xff, 0x00, 0x05 // 0x000500ff, what Samba answers (no read-only flag: Windows may treat such a volume differently)
		out[4] = 255
		for i := range 4 {
			out[8+i] = byte(uint32(len(name)) >> (8 * i))
		}
		copy(out[12:], name)
		return out
	case wire.FileFsSizeInformation:
		out := make([]byte, 24)
		put64LE(out[0:8], 1<<40)
		put64LE(out[8:16], 1<<39)
		out[16] = 1
		out[18] = 1
		return out
	case wire.FileFsDeviceInformation: // a disk that cannot be written to
		out := make([]byte, 8)
		out[0] = 0x07
		out[4] = 0x22
		return out
	case wire.FileFsFullSizeInformation:
		out := make([]byte, 32)
		put64LE(out[0:8], 1<<40)
		put64LE(out[8:16], 1<<39)
		put64LE(out[16:24], 1<<39)
		out[24] = 1
		out[28], out[29] = 0x00, 0x02 // 512 bytes per sector
		return out
	case wire.FileFsSectorSizeInformation:
		out := make([]byte, 28)
		for _, at := range []int{0, 4, 8, 12} {
			out[at+1] = 0x02 // 512
		}
		out[16] = 0x03
		return out
	case wire.FileFsVolumeInformation:
		// FileFsVolumeInformation: creation time (8), serial number (4), label length (4), supports objects (1), reserved (1), the label: 18 bytes
		// before the label. The library once put the label at byte 16, which Windows takes for a damaged answer.
		label := wire.UTF16ToBytes("ISO")
		out := make([]byte, 18+len(label))
		out[8] = 0x04
		for i := range 4 {
			out[12+i] = byte(uint32(len(label)) >> (8 * i))
		}
		copy(out[18:], label)
		return out
	default:
		return nil
	}
}

func (c *conn) handleSetInfo(ctx context.Context, msg []byte, tr *tree) uint32 {
	var req wire.SetInfoRequest
	if err := req.Parse(msg); err != nil {
		return c.errBody(wire.StatusInvalidParameter)
	}
	oh, ok := tr.opens[req.FileId]
	if !ok {
		return c.errBody(wire.StatusInvalidHandle)
	}

	if req.InfoType == wire.InfoFile {
		switch req.FileInfoClass {
		case wire.FileDispositionInformation:
			if len(req.Buffer) < 1 {
				return c.errBody(wire.StatusInvalidParameter)
			}
			oh.deletePending = req.Buffer[0] != 0

		case wire.FileBasicInfoClass:
			var bi wire.FileBasicInformation
			if err := bi.Parse(req.Buffer); err != nil {
				return c.errBody(wire.StatusInvalidParameter)
			}
			if si, ok := oh.h.(vfs.SetInfoer); ok {
				var req vfs.SetInfoRequest
				if bi.CreationTime != 0 {
					t := wire.FiletimeToTime(bi.CreationTime)
					req.CreationTime = &t
				}
				if bi.LastAccessTime != 0 {
					t := wire.FiletimeToTime(bi.LastAccessTime)
					req.LastAccessTime = &t
				}
				if bi.LastWriteTime != 0 {
					t := wire.FiletimeToTime(bi.LastWriteTime)
					req.LastWriteTime = &t
				}
				if bi.ChangeTime != 0 {
					t := wire.FiletimeToTime(bi.ChangeTime)
					req.ChangeTime = &t
				}
				if bi.FileAttributes != 0 {
					req.Attributes = &bi.FileAttributes
				}
				if err := si.SetInfo(ctx, &req); err != nil {
					return c.errBody(osErrToStatus(err))
				}
			}

		case wire.FileEndOfFileInformation:
			if len(req.Buffer) < 8 {
				return c.errBody(wire.StatusInvalidParameter)
			}
			newSize := readLE64(req.Buffer[0:8])
			if si, ok := oh.h.(vfs.SetInfoer); ok {
				if err := si.SetInfo(ctx, &vfs.SetInfoRequest{EndOfFile: &newSize}); err != nil {
					return c.errBody(osErrToStatus(err))
				}
			}

		case wire.FileRenameInformation:
			if len(req.Buffer) < 20 {
				return c.errBody(wire.StatusInvalidParameter)
			}
			replaceIfExist := req.Buffer[0] != 0
			fnLen := le32(req.Buffer[16:20])
			if int(20+fnLen) > len(req.Buffer) {
				return c.errBody(wire.StatusInvalidParameter)
			}
			newName := wire.UTF16FromBytes(req.Buffer[20 : 20+fnLen])
			if rn, ok := oh.h.(vfs.Renamer); ok {
				if err := rn.Rename(ctx, newName, replaceIfExist); err != nil {
					return c.errBody(osErrToStatus(err))
				}
			} else {
				return c.errBody(wire.StatusNotSupported)
			}

		default:
			return c.errBody(wire.StatusNotSupported)
		}
		c.out = wire.SetInfoResponseAppend(c.out)
		return wire.StatusSuccess
	}
	return c.errBody(wire.StatusNotSupported)
}

func (c *conn) handleFlush(_ context.Context, _ []byte, _ *tree) uint32 {
	c.out = wire.FlushResponseAppend(c.out)
	return wire.StatusSuccess
}

func boolToU8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

func put64LE(dst []byte, v uint64) {
	for i := range 8 {
		dst[i] = byte(v >> (8 * i))
	}
}

func putLE64(dst []byte, v uint64) {
	for i := range 8 {
		dst[i] = byte(v >> (8 * i))
	}
}

func readLE64(b []byte) int64 {
	return int64(uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56)
}
