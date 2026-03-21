package toolauth

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

func emptySessionStateArchive() ([]byte, error) {
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.Close(); err != nil {
		gzipWriter.Close()
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func EmptySessionStateArchive() ([]byte, error) {
	return emptySessionStateArchive()
}

func archiveHasEntries(archive []byte) (bool, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return false, err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == nil {
			switch header.Typeflag {
			case tar.TypeDir, tar.TypeXGlobalHeader, tar.TypeXHeader:
				continue
			default:
				return true, nil
			}
		}
		if err == io.EOF {
			return false, nil
		}
		return false, err
	}
}

func archiveSHA256(archive []byte) string {
	sum := sha256.Sum256(archive)
	return hex.EncodeToString(sum[:])
}
