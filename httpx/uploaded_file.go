package httpx

import (
	"io"
	"net/textproto"
)

// IUploadedFile 表示框架级上传文件抽象，屏蔽具体 Web 框架的文件头实现。
type IUploadedFile interface {
	Filename() string
	Size() int64
	Header() textproto.MIMEHeader
	Open() (io.ReadCloser, error)
}
