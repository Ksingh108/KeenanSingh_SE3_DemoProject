param(
    [Parameter(Position=0)]
    [string]$Command = "help"
)

switch ($Command) {
    "docker-reset" {
        Write-Host "Performing complete Docker reset..." -ForegroundColor Green
        Write-Host "Stopping containers and removing volumes..." -ForegroundColor Yellow
        docker compose down -v
        docker volume prune -f
        Write-Host "Rebuilding from scratch..." -ForegroundColor Yellow
        docker compose up --build
    }

    "docker-restart" {
        Write-Host "Stopping and rebuilding Docker containers..." -ForegroundColor Green
        docker compose down
        docker compose up --build
    }
    
    "docker-up" {
        Write-Host "Starting Docker containers..." -ForegroundColor Green
        docker compose up --build
    }
    
    "docker-down" {
        Write-Host "Stopping Docker containers..." -ForegroundColor Green
        docker compose down
    }
    
    "docker-logs" {
        Write-Host "Showing Docker logs..." -ForegroundColor Green
        docker compose logs -f
    }
    
    "test" {
        Write-Host "Running tests..." -ForegroundColor Green
        go test -v -race ./...
    }

    "test-integration" {
        Write-Host "Running Postgres integration tests (requires Docker)..." -ForegroundColor Green
        go test -v -race -tags integration -count=1 ./internal/repository/postgres/...
    }


    
    "test-coverage" {
        Write-Host "Running tests with coverage..." -ForegroundColor Green
        
        # Create test reports directory structure
        if (!(Test-Path "Test Reports")) { New-Item -ItemType Directory -Name "Test Reports" }
        
        # Generate readable timestamp and create run-specific folder
        $timestamp = Get-Date -Format "yyyy-MM-dd_HH-mm-ss"
        $runFolder = "Test Reports/Run_$timestamp"
        New-Item -ItemType Directory -Path $runFolder -Force
        
        # Define file paths within the run folder
        $htmlFile = "$runFolder/coverage.html"
        $summaryFile = "$runFolder/test_summary.html"
        
        # Run tests and capture results
        Write-Host "Running unit tests..." -ForegroundColor Cyan
        $testOutput = go test -v -coverprofile="temp_coverage.out" -json ./...
        
        # Generate HTML coverage report
        Write-Host "Generating coverage report..." -ForegroundColor Cyan
        go tool cover -html="temp_coverage.out" -o $htmlFile
        Remove-Item "temp_coverage.out" -ErrorAction SilentlyContinue
        
        # Generate test summary report
        Write-Host "Generating test summary report..." -ForegroundColor Cyan
        $jsonContent = $testOutput | ConvertFrom-Json
        $testResults = @()
        $passCount = 0
        $failCount = 0
        $skipCount = 0
        
        foreach ($line in $jsonContent) {
            if ($line.Action -eq "pass" -and $line.Test) {
                $testResults += @{
                    Package = $line.Package
                    Test = $line.Test
                    Status = "PASS"
                    Time = $line.Elapsed
                }
                $passCount++
            }
            elseif ($line.Action -eq "fail" -and $line.Test) {
                $testResults += @{
                    Package = $line.Package
                    Test = $line.Test
                    Status = "FAIL"
                    Time = $line.Elapsed
                }
                $failCount++
            }
            elseif ($line.Action -eq "skip" -and $line.Test) {
                $testResults += @{
                    Package = $line.Package
                    Test = $line.Test
                    Status = "SKIP"
                    Time = $line.Elapsed
                }
                $skipCount++
            }
        }
        
        # Create HTML summary report
        $htmlContent = @"
<!DOCTYPE html>
<html>
<head>
    <title>Test Results Summary - $timestamp</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { background: #f8f9fa; padding: 20px; border-radius: 5px; margin-bottom: 20px; }
        .stats { display: flex; gap: 20px; margin-bottom: 20px; }
        .stat-box { padding: 15px; border-radius: 5px; text-align: center; min-width: 100px; }
        .pass { background: #d4edda; color: #155724; }
        .fail { background: #f8d7da; color: #721c24; }
        .skip { background: #fff3cd; color: #856404; }
        table { width: 100%; border-collapse: collapse; margin-top: 20px; }
        th, td { padding: 10px; text-align: left; border-bottom: 1px solid #ddd; }
        th { background: #f8f9fa; }
        .status-pass { color: #28a745; font-weight: bold; }
        .status-fail { color: #dc3545; font-weight: bold; }
        .status-skip { color: #ffc107; font-weight: bold; }
    </style>
</head>
<body>
    <div class="header">
        <h1>Test Results Summary</h1>
        <p>Generated: $(Get-Date -Format "yyyy-MM-dd HH:mm:ss")</p>
    </div>
    
    <div class="stats">
        <div class="stat-box pass">
            <h3>$passCount</h3>
            <p>Passed</p>
        </div>
        <div class="stat-box fail">
            <h3>$failCount</h3>
            <p>Failed</p>
        </div>
        <div class="stat-box skip">
            <h3>$skipCount</h3>
            <p>Skipped</p>
        </div>
    </div>
    
    <table>
        <thead>
            <tr>
                <th>Package</th>
                <th>Test Case</th>
                <th>Status</th>
                <th>Duration</th>
            </tr>
        </thead>
        <tbody>
"@
        
        foreach ($result in $testResults) {
            $statusClass = "status-" + $result.Status.ToLower()
            $duration = if ($result.Time) { [math]::Round($result.Time, 3).ToString() + "s" } else { "N/A" }
            $htmlContent += "            <tr>`n"
            $htmlContent += "                <td>$($result.Package)</td>`n"
            $htmlContent += "                <td>$($result.Test)</td>`n"
            $htmlContent += "                <td class=`"$statusClass`">$($result.Status)</td>`n"
            $htmlContent += "                <td>$duration</td>`n"
            $htmlContent += "            </tr>`n"
        }
        
        $htmlContent += @"
        </tbody>
    </table>
</body>
</html>
"@
        
        $htmlContent | Out-File -FilePath $summaryFile -Encoding UTF8
        
        Write-Host "" -ForegroundColor White
        Write-Host "Test Results Summary:" -ForegroundColor Green
        Write-Host "- Passed: $passCount" -ForegroundColor Green
        Write-Host "- Failed: $failCount" -ForegroundColor Red
        Write-Host "- Skipped: $skipCount" -ForegroundColor Yellow
        Write-Host "" -ForegroundColor White
        Write-Host "Reports saved to: $runFolder" -ForegroundColor Cyan
        Write-Host "" -ForegroundColor White
        Write-Host "Generated Files:" -ForegroundColor Yellow
        Write-Host "- test_summary.html    (Test pass/fail overview)" -ForegroundColor White
        Write-Host "- coverage.html        (Code coverage report)" -ForegroundColor White
        Write-Host "" -ForegroundColor White
        Write-Host "Open HTML files in browser to view detailed reports" -ForegroundColor Cyan
    }

    "clean" {
        Write-Host "Cleaning up..." -ForegroundColor Green
        if (Test-Path "Test Reports") { Remove-Item -Recurse -Force "Test Reports" }
        docker compose down
        docker system prune -f
        Write-Host "Cleanup complete" -ForegroundColor Yellow
    }
    
    "help" {
        Write-Host "Gringotts Bank Development Helper" -ForegroundColor Cyan
        Write-Host ""
        Write-Host "Docker Commands:" -ForegroundColor Yellow
        Write-Host "  docker-reset     - Complete reset (removes volumes, rebuilds fresh)"
        Write-Host "  docker-restart   - Stop and rebuild containers"
        Write-Host "  docker-up        - Start containers"
        Write-Host "  docker-down      - Stop containers"
        Write-Host "  docker-logs      - Show container logs"
        Write-Host ""
        Write-Host "Testing Commands:" -ForegroundColor Yellow
        Write-Host "  test             - Run unit tests"
        Write-Host "  test-integration - Run Postgres integration tests (requires Docker)"
        Write-Host "  test-coverage    - Run tests with coverage report"
        Write-Host ""
        Write-Host "Utility Commands:" -ForegroundColor Yellow
        Write-Host "  clean            - Clean up test reports and Docker"
        Write-Host ""
        Write-Host "Usage: .\dev.ps1 <command>" -ForegroundColor Green
        Write-Host "Example: .\dev.ps1 docker-restart" -ForegroundColor Green
    }
    
    default {
        Write-Host "Unknown command: $Command" -ForegroundColor Red
        Write-Host "Run '.\dev.ps1 help' for available commands"
    }
}