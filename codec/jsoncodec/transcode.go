package jsoncodec

import (
	"encoding/json"
	stderrors "errors"
	"io"

	"gochen/errors"
)

// Transcode 通过 JSON 序列化/反序列化将 src 转换为目标类型 T。
func Transcode[T any](src any, opts ...Option) (T, error) {
	var target T
	if err := TranscodeInto(src, &target, opts...); err != nil {
		var zero T
		return zero, err
	}
	return target, nil
}

// TranscodeInto 通过 JSON 序列化/反序列化将 src 转换为 dest 指向的已有变量。
func TranscodeInto[T any](src any, dest *T, opts ...Option) error {
	if src == nil {
		return errors.NewCode(errors.InvalidInput, "transcode source cannot be nil")
	}
	if dest == nil {
		return errors.NewCode(errors.InvalidInput, "transcode destination cannot be nil")
	}

	data, err := Encode(src)
	if err != nil {
		return errors.Wrap(err, errors.InvalidInput, "failed to transcode JSON input")
	}

	codec := New[T](opts...)
	return codec.DecodeInto(data, dest)
}

func decodeWithReader(dec *json.Decoder, target any, cfg config) error {
	if cfg.useNumber {
		dec.UseNumber()
	}
	if cfg.disallowUnknownFields {
		dec.DisallowUnknownFields()
	}

	if err := dec.Decode(target); err != nil {
		return errors.Wrap(err, errors.InvalidInput, "failed to decode JSON")
	}

	if cfg.rejectTrailingData {
		var extra any
		err := dec.Decode(&extra)
		if err == nil {
			return errors.NewCode(errors.InvalidInput, "unexpected trailing data after JSON value")
		}
		if !stderrors.Is(err, io.EOF) {
			return errors.Wrap(err, errors.InvalidInput, "unexpected trailing data after JSON value")
		}
	}
	return nil
}
