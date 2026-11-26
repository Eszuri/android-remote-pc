$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$AndroidDir = $PSScriptRoot
$BuildDir = Join-Path $AndroidDir "build"
$AssetsStage = Join-Path $BuildDir "assets"
$ClassesDir = Join-Path $BuildDir "classes"
$GenDir = Join-Path $BuildDir "gen"
$OutBinDir = Join-Path $Root "bin"
$WebUIDir = Join-Path $Root "internal\webui"
$AppVer = if (Test-Path "$Root\VERSION") { (Get-Content "$Root\VERSION" -Raw).Trim() } else { "1.0.0" }
$vParts = $AppVer.Split('.')
$VerCode = ([int]$vParts[0] * 10000) + ([int]$vParts[1] * 100) + [int]$vParts[2]

$SdkCandidates = @($env:ANDROID_HOME, $env:ANDROID_SDK_ROOT, "$env:LOCALAPPDATA\Android\Sdk") | Where-Object { $_ -and (Test-Path $_) }
if (-not $SdkCandidates) { throw "Android SDK not found" }
$SdkDir = $SdkCandidates[0]

$BtDir = Get-ChildItem "$SdkDir\build-tools" -Directory -ErrorAction SilentlyContinue | Sort-Object Name -Descending | Where-Object { Test-Path "$($_.FullName)\aapt2.exe" } | Select-Object -First 1
if (-not $BtDir) { throw "Android build-tools (aapt2) not found in $SdkDir" }
$BuildTools = $BtDir.FullName

$PlatformJar = "$SdkDir\platforms\android-35\android.jar"
if (-not (Test-Path $PlatformJar)) {
  $Plat = Get-ChildItem "$SdkDir\platforms" -Directory -ErrorAction SilentlyContinue | Sort-Object Name -Descending | Where-Object { Test-Path "$($_.FullName)\android.jar" } | Select-Object -First 1
  if (-not $Plat) { throw "Android platform android.jar not found in $SdkDir" }
  $PlatformJar = "$($Plat.FullName)\android.jar"
}

if (Test-Path "C:\Program Files\Android\Android Studio\jbr\bin\javac.exe") {
  $env:JAVA_HOME = "C:\Program Files\Android\Android Studio\jbr"
  $JbrBin = "$env:JAVA_HOME\bin"
} elseif ($env:JAVA_HOME -and (Test-Path "$env:JAVA_HOME\bin\javac.exe")) {
  $JbrBin = "$env:JAVA_HOME\bin"
} else {
  $JavacCmd = Get-Command javac.exe -ErrorAction SilentlyContinue
  if (-not $JavacCmd) { throw "JDK (javac.exe) not found" }
  $JbrBin = Split-Path -Parent $JavacCmd.Source
  $env:JAVA_HOME = Split-Path -Parent $JbrBin
}

if (Test-Path $BuildDir) { Remove-Item -Recurse -Force $BuildDir }
New-Item -ItemType Directory -Force -Path $AssetsStage, $ClassesDir, $GenDir, $OutBinDir | Out-Null

Copy-Item "$WebUIDir\index.html" "$AssetsStage\index.html"
Copy-Item "$WebUIDir\favicon.ico" "$AssetsStage\favicon.ico"
Copy-Item "$WebUIDir\manifest.json" "$AssetsStage\manifest.json"
Copy-Item "$WebUIDir\sw.js" "$AssetsStage\sw.js"
Copy-Item -Recurse "$WebUIDir\css" "$AssetsStage\css"
Copy-Item -Recurse "$WebUIDir\img" "$AssetsStage\img"
Copy-Item -Recurse "$WebUIDir\js" "$AssetsStage\js"
Copy-Item -Recurse "$WebUIDir\vendor" "$AssetsStage\vendor"

$ResZip = Join-Path $BuildDir "res.zip"
& "$BuildTools\aapt2.exe" compile --dir "$AndroidDir\res" -o $ResZip
if ($LASTEXITCODE -ne 0) { throw "aapt2 compile failed" }

$UnsignedApk = Join-Path $BuildDir "unaligned.apk"
& "$BuildTools\aapt2.exe" link `
  -I $PlatformJar `
  --manifest "$AndroidDir\AndroidManifest.xml" `
  --version-code $VerCode `
  --version-name $AppVer `
  --java $GenDir `
  -o $UnsignedApk `
  $ResZip
if ($LASTEXITCODE -ne 0) { throw "aapt2 link failed" }

$JavaFiles = Get-ChildItem -Path $GenDir, "$AndroidDir\src" -Recurse -Filter "*.java" | ForEach-Object { $_.FullName }
& "$JbrBin\javac.exe" --release 11 -encoding UTF-8 -cp $PlatformJar -d $ClassesDir $JavaFiles
if ($LASTEXITCODE -ne 0) { throw "javac failed" }

$ClassFiles = Get-ChildItem -Path $ClassesDir -Recurse -Filter "*.class" | ForEach-Object { $_.FullName }
& "$BuildTools\d8.bat" --min-api 26 --lib $PlatformJar --output $BuildDir $ClassFiles
if ($LASTEXITCODE -ne 0) { throw "d8 failed" }

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zip = [System.IO.Compression.ZipFile]::Open($UnsignedApk, [System.IO.Compression.ZipArchiveMode]::Update)
[System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, "$BuildDir\classes.dex", "classes.dex") | Out-Null
Get-ChildItem -Path $AssetsStage -Recurse -File | ForEach-Object {
  $rel = $_.FullName.Substring($AssetsStage.Length + 1).Replace('\', '/')
  [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile(
    $zip,
    $_.FullName,
    "assets/$rel",
    [System.IO.Compression.CompressionLevel]::Optimal
  ) | Out-Null
}
$zip.Dispose()

$AlignedApk = Join-Path $BuildDir "aligned.apk"
& "$BuildTools\zipalign.exe" -f -p 4 $UnsignedApk $AlignedApk
if ($LASTEXITCODE -ne 0) { throw "zipalign failed" }

$Keystore = Join-Path $AndroidDir "arpc-release.jks"
if (-not (Test-Path $Keystore)) {
  & "$JbrBin\keytool.exe" -genkeypair -v `
    -keystore $Keystore `
    -alias arpc `
    -keyalg RSA -keysize 2048 -validity 10000 `
    -storepass arpc123 -keypass arpc123 `
    -dname "CN=ARPC, O=ARPC, C=ID" | Out-Null
}

$FinalApk = Join-Path $OutBinDir "ARPC.apk"
& "$BuildTools\apksigner.bat" sign `
  --ks $Keystore `
  --ks-key-alias arpc `
  --ks-pass pass:arpc123 `
  --key-pass pass:arpc123 `
  --v4-signing-enabled false `
  --out $FinalApk `
  $AlignedApk
if ($LASTEXITCODE -ne 0) { throw "apksigner failed" }

Remove-Item -Recurse -Force $BuildDir -ErrorAction SilentlyContinue
Remove-Item -Force "$FinalApk.idsig" -ErrorAction SilentlyContinue

& "$BuildTools\apksigner.bat" verify -v $FinalApk | Select-String "Verified"
Get-Item $FinalApk | Select-Object FullName, Length
