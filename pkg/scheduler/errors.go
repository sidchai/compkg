package scheduler

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// grpcStatus 提取 err 的 gRPC code 字符串；非 gRPC 错误返回 ok=false。
func grpcStatus(err error) (string, bool) {
	st, ok := status.FromError(err)
	if !ok {
		return "", false
	}
	return st.Code().String(), true
}

// isNotFoundErr 判断 err 是否是 gRPC NotFound。
func isNotFoundErr(err error) bool {
	st, ok := status.FromError(err)
	return ok && st.Code() == codes.NotFound
}

// isAlreadyExistsErr 判断 err 是否是 gRPC AlreadyExists。
func isAlreadyExistsErr(err error) bool {
	st, ok := status.FromError(err)
	return ok && st.Code() == codes.AlreadyExists
}
