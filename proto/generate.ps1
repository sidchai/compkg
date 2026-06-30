# =====================================================================
# compkg proto stub generator (Windows / PowerShell)
# =====================================================================
# 输出（与各 .proto 同目录，paths=source_relative）：
#   proto/scheduler/v1/scheduler.proto
#   proto/scheduler/v1/scheduler.pb.go         (--go_out)
#   proto/scheduler/v1/scheduler_grpc.pb.go    (--go-grpc_out)
#   proto/scheduler/v1/scheduler.pb.gw.go      (--grpc-gateway_out)   ← 新增
#   proto/scheduler/v1/scheduler.swagger.json  (--openapiv2_out)      ← 新增
#
# 工具链：
#   - protoc                      https://github.com/protocolbuffers/protobuf/releases
#   - protoc-gen-go               go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   - protoc-gen-go-grpc          go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
#   - protoc-gen-grpc-gateway     go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
#   - protoc-gen-openapiv2        go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest
#
# 依赖 proto：proto/google/api/{annotations,http,httpbody}.proto（来自 googleapis）
# =====================================================================

$ErrorActionPreference = "Stop"

$ProtoRoot    = $PSScriptRoot
$ProtoRootFwd = ($ProtoRoot -replace '\\','/')

Write-Host "[1/3] Checking toolchain..." -ForegroundColor Cyan
$protocVersion   = & protoc --version
$genGoPath       = (Get-Command protoc-gen-go             -ErrorAction Stop).Source
$genGoGrpcPath   = (Get-Command protoc-gen-go-grpc        -ErrorAction Stop).Source
$genGwPath       = (Get-Command protoc-gen-grpc-gateway   -ErrorAction Stop).Source
$genOpenapiPath  = (Get-Command protoc-gen-openapiv2      -ErrorAction Stop).Source
Write-Host "  protoc:                  $protocVersion"
Write-Host "  protoc-gen-go:           $genGoPath"
Write-Host "  protoc-gen-go-grpc:      $genGoGrpcPath"
Write-Host "  protoc-gen-grpc-gateway: $genGwPath"
Write-Host "  protoc-gen-openapiv2:    $genOpenapiPath"

Write-Host "[2/3] Generating Go stubs (in-place, paths=source_relative)..." -ForegroundColor Cyan
Push-Location $ProtoRoot
try {
    # 仅业务 proto 参与 stub 生成；google/api、google/protobuf 仅作 import 依赖
    $allProtos = Get-ChildItem -Path . -Filter "*.proto" -Recurse
    $protoFiles = [System.Collections.Generic.List[string]]::new()
    foreach ($f in $allProtos) {
        $rel = $f.FullName.Substring($ProtoRoot.Length + 1).Replace('\','/')
        if ($rel.StartsWith('google/')) { continue }
        $protoFiles.Add($rel)
        Write-Host "  -> $rel"
    }
    if ($protoFiles.Count -eq 0) { throw "no business proto files found under $ProtoRoot" }

    Write-Host "  proto files to compile: $($protoFiles -join ', ')"

    & protoc `
        --proto_path=$ProtoRootFwd `
        --go_out=. `
        --go_opt=paths=source_relative `
        --go_opt=Mgoogle/protobuf/descriptor.proto=google.golang.org/protobuf/types/descriptorpb `
        --go_opt=Mgoogle/api/annotations.proto=google.golang.org/genproto/googleapis/api/annotations `
        --go_opt=Mgoogle/api/http.proto=google.golang.org/genproto/googleapis/api/annotations `
        --go_opt=Mgoogle/api/httpbody.proto=google.golang.org/genproto/googleapis/api/httpbody `
        --go-grpc_out=. `
        --go-grpc_opt=paths=source_relative `
        --go-grpc_opt=Mgoogle/protobuf/descriptor.proto=google.golang.org/protobuf/types/descriptorpb `
        --go-grpc_opt=Mgoogle/api/annotations.proto=google.golang.org/genproto/googleapis/api/annotations `
        --go-grpc_opt=Mgoogle/api/http.proto=google.golang.org/genproto/googleapis/api/annotations `
        --go-grpc_opt=Mgoogle/api/httpbody.proto=google.golang.org/genproto/googleapis/api/httpbody `
        --grpc-gateway_out=. `
        --grpc-gateway_opt=paths=source_relative `
        --grpc-gateway_opt=generate_unbound_methods=false `
        --grpc-gateway_opt=Mgoogle/protobuf/descriptor.proto=google.golang.org/protobuf/types/descriptorpb `
        --grpc-gateway_opt=Mgoogle/api/annotations.proto=google.golang.org/genproto/googleapis/api/annotations `
        --grpc-gateway_opt=Mgoogle/api/http.proto=google.golang.org/genproto/googleapis/api/annotations `
        --grpc-gateway_opt=Mgoogle/api/httpbody.proto=google.golang.org/genproto/googleapis/api/httpbody `
        --openapiv2_out=. `
        --openapiv2_opt=logtostderr=true `
        --openapiv2_opt=allow_merge=false `
        --openapiv2_opt=Mgoogle/protobuf/descriptor.proto=google.golang.org/protobuf/types/descriptorpb `
        --openapiv2_opt=Mgoogle/api/annotations.proto=google.golang.org/genproto/googleapis/api/annotations `
        --openapiv2_opt=Mgoogle/api/http.proto=google.golang.org/genproto/googleapis/api/annotations `
        --openapiv2_opt=Mgoogle/api/httpbody.proto=google.golang.org/genproto/googleapis/api/httpbody `
        $protoFiles
} finally {
    Pop-Location
}

if ($LASTEXITCODE -ne 0) {
    Write-Host "[X] protoc failed with exit code $LASTEXITCODE" -ForegroundColor Red
    exit $LASTEXITCODE
}

Write-Host "[3/3] Done" -ForegroundColor Green
Get-ChildItem -Path $ProtoRoot -Recurse -Include "*.pb.go","*.pb.gw.go","*.swagger.json" | ForEach-Object {
    $rel  = $_.FullName.Substring($ProtoRoot.Length + 1)
    $size = [math]::Round($_.Length / 1KB, 1)
    Write-Host ('  {0,-55} {1,7} KB' -f $rel, $size)
}
