package service

// External integration tests use the real one-task downloader without starting
// its unbounded background loop. This symbol is absent from production builds.
var DownloadTaskResultForTest = DownloadManager.downloadTaskResult
