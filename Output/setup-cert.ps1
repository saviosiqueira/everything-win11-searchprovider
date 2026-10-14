# Generates and trusts local server TLS certificate and imports the MSIX signing certificate.

param(
    [switch]$Force
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Relaunch PowerShell as Admin if not already elevated

$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    $scriptPath = if ($PSCommandPath) { $PSCommandPath } else { $MyInvocation.MyCommand.Path }
    $powershellExe = if ($PSVersionTable.PSEdition -eq 'Core') { 'pwsh.exe' } else { 'powershell.exe' }
    $argList = @("-ExecutionPolicy", "Bypass", "-File", "`"$scriptPath`"")
    if ($Force) { $argList += "-Force" }

    Start-Process $powershellExe -ArgumentList $argList -Verb RunAs
    exit 0
}

# Localhost TLS Server Certificate Setup

$targetDir = Join-Path $env:LOCALAPPDATA "EverythingSearchProvider"
if (-not (Test-Path $targetDir)) {
    New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
}

$certFile = Join-Path $targetDir "cert.pem"
$keyFile  = Join-Path $targetDir "key.pem"
$rootCer  = Join-Path $targetDir "root.cer"

if ($Force -or (-not (Test-Path $certFile)) -or (-not (Test-Path $keyFile)) -or (-not (Test-Path $rootCer))) {
    $cert = New-SelfSignedCertificate `
        -Subject "CN=localhost" `
        -DnsName "localhost", "127.0.0.1", "::1" `
        -KeyAlgorithm RSA `
        -KeyLength 2048 `
        -HashAlgorithm SHA256 `
        -NotAfter (Get-Date).AddYears(5) `
        -CertStoreLocation "Cert:\CurrentUser\My" `
        -KeyExportPolicy Exportable

    # Export Public Certificate to DER (root.cer)
    $certBytes = $cert.Export([System.Security.Cryptography.X509Certificates.X509ContentType]::Cert)
    [System.IO.File]::WriteAllBytes($rootCer, $certBytes)

    # Export Public Certificate to PEM (cert.pem)
    $base64Cert = [Convert]::ToBase64String($certBytes, [System.Base64FormattingOptions]::InsertLineBreaks)
    $pemCert = "-----BEGIN CERTIFICATE-----`n$base64Cert`n-----END CERTIFICATE-----`n"
    [System.IO.File]::WriteAllText($certFile, $pemCert, [System.Text.Encoding]::ASCII)

    # Export Private Key to PEM (key.pem)
    $rsa = [System.Security.Cryptography.X509Certificates.RSACertificateExtensions]::GetRSAPrivateKey($cert)
    if ($rsa.PSObject.Methods.Match("ExportPkcs8PrivateKey").Count -gt 0) {
        $pkcs8Bytes = $rsa.ExportPkcs8PrivateKey()
        $base64Key = [Convert]::ToBase64String($pkcs8Bytes, [System.Base64FormattingOptions]::InsertLineBreaks)
        $pemKey = "-----BEGIN PRIVATE KEY-----`n$base64Key`n-----END PRIVATE KEY-----`n"
    } else {
        # Fallback DER integer & header encoders for PowerShell 5.1 (.NET Framework 4.x)
        function ConvertTo-DerHeader([byte]$tag, [byte[]]$data) {
            $len = $data.Length
            if ($len -lt 128) {
                $header = [byte[]]@($tag, [byte]$len)
            } elseif ($len -le 0xFF) {
                $header = [byte[]]@($tag, 0x81, [byte]$len)
            } elseif ($len -le 0xFFFF) {
                $header = [byte[]]@($tag, 0x82, [byte]($len -shr 8), [byte]($len -band 0xFF))
            } else {
                $header = [byte[]]@($tag, 0x83, [byte]($len -shr 16), [byte](($len -shr 8) -band 0xFF), [byte]($len -band 0xFF))
            }
            return [byte[]]($header + $data)
        }

        function ConvertTo-DerInteger([byte[]]$bytes) {
            $start = 0
            while ($start -lt $bytes.Length - 1 -and $bytes[$start] -eq 0) { $start++ }
            $trimmed = $bytes[$start..($bytes.Length - 1)]
            if ($trimmed[0] -ge 0x80) {
                $data = [byte[]](@([byte]0) + $trimmed)
            } else {
                $data = $trimmed
            }
            return ConvertTo-DerHeader 0x02 $data
        }

        $p = $rsa.ExportParameters($true)
        $v  = ConvertTo-DerInteger @([byte]0)
        $m  = ConvertTo-DerInteger $p.Modulus
        $e  = ConvertTo-DerInteger $p.Exponent
        $d  = ConvertTo-DerInteger $p.D
        $p1 = ConvertTo-DerInteger $p.P
        $p2 = ConvertTo-DerInteger $p.Q
        $e1 = ConvertTo-DerInteger $p.DP
        $e2 = ConvertTo-DerInteger $p.DQ
        $c1 = ConvertTo-DerInteger $p.InverseQ

        $seq = $v + $m + $e + $d + $p1 + $p2 + $e1 + $e2 + $c1
        $der = ConvertTo-DerHeader 0x30 $seq
        $base64Key = [Convert]::ToBase64String($der, [System.Base64FormattingOptions]::InsertLineBreaks)
        $pemKey = "-----BEGIN RSA PRIVATE KEY-----`n$base64Key`n-----END RSA PRIVATE KEY-----`n"
    }
    [System.IO.File]::WriteAllText($keyFile, $pemKey, [System.Text.Encoding]::ASCII)

    # Cleanup temporary cert from My store
    Remove-Item -Path "Cert:\CurrentUser\My\$($cert.Thumbprint)" -Force
    Write-Host "TLS Certificate files created in $targetDir" -ForegroundColor Green
} else {
    Write-Host "TLS Certificate files already exist." -ForegroundColor Cyan
}

# Import into System Root Stores
Import-Certificate -CertStoreLocation "Cert:\LocalMachine\Root" -FilePath $rootCer | Out-Null
Import-Certificate -CertStoreLocation "Cert:\CurrentUser\Root" -FilePath $rootCer | Out-Null
Write-Host "TLS Certificate trusted in LocalMachine\Root and CurrentUser\Root." -ForegroundColor Green

# MSIX Installer Signing Certificate Setup

$msixCertPath = Join-Path $PSScriptRoot "..\_signing.cer"
if (-not (Test-Path $msixCertPath)) {
    $msixCertPath = Join-Path $PSScriptRoot "_signing.cer"
}
if (-not (Test-Path $msixCertPath)) {
    $msixCertPath = ".\_signing.cer"
}

if (Test-Path $msixCertPath) {
    Import-Certificate -CertStoreLocation "Cert:\LocalMachine\TrustedPeople" -FilePath $msixCertPath | Out-Null
    Write-Host "MSIX Certificate ($msixCertPath) trusted in LocalMachine\TrustedPeople." -ForegroundColor Green
} else {
    Write-Host "'_signing.cer' not found. Run build.ps1 first if installing MSIX." -ForegroundColor Yellow
}
