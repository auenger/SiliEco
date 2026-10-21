$ErrorActionPreference = "Stop"
$baseUrl = if ($env:SILIECO_DOWNLOAD_BASE_URL) { $env:SILIECO_DOWNLOAD_BASE_URL.TrimEnd('/') } else { "http://yilia.me:6688/cli" }

$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq [System.Runtime.InteropServices.Architecture]::Arm64) { "arm64" } else { "amd64" }
$archive = "silieco-windows-$arch.zip"
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("silieco-cli-" + [guid]::NewGuid().ToString("N"))
$binDir = if ($env:SILIECO_BIN_DIR) { $env:SILIECO_BIN_DIR } else { Join-Path $env:LOCALAPPDATA "Silieco\bin" }

New-Item -ItemType Directory -Force -Path $tempDir, $binDir | Out-Null
try {
  Write-Host "==> 正在从 $baseUrl 下载 Silieco CLI (windows/$arch)..."
  $archivePath = Join-Path $tempDir $archive
  Invoke-WebRequest -UseBasicParsing "$baseUrl/$archive" -OutFile $archivePath
  Invoke-WebRequest -UseBasicParsing "$baseUrl/checksums.txt" -OutFile (Join-Path $tempDir "checksums.txt")

  $line = Get-Content (Join-Path $tempDir "checksums.txt") | Where-Object { $_ -match "\s+$([regex]::Escape($archive))$" } | Select-Object -First 1
  if (-not $line) { throw "校验文件中没有 $archive" }
  $expected = ($line -split '\s+')[0].ToLowerInvariant()
  $actual = (Get-FileHash -Algorithm SHA256 $archivePath).Hash.ToLowerInvariant()
  if ($actual -ne $expected) { throw "下载文件校验失败" }

  Expand-Archive -Force $archivePath $tempDir
  Copy-Item -Force (Join-Path $tempDir "silieco.exe") (Join-Path $binDir "silieco.exe")

  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (($userPath -split ';') -notcontains $binDir) {
    [Environment]::SetEnvironmentVariable("Path", (($userPath.TrimEnd(';') + ';' + $binDir).TrimStart(';')), "User")
  }
  $env:Path = "$binDir;$env:Path"
  Write-Host "==> Silieco CLI 已安装到 $binDir\silieco.exe"
  & (Join-Path $binDir "silieco.exe") version
  Write-Host "`n下一步："
  Write-Host "  silieco setup self-host --server-url http://yilia.me:6688 --app-url http://yilia.me:6688"
} finally {
  Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue
}
