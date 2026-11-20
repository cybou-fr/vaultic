package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"

    "github.com/restic/restic/internal/global"
)

// These are restic's native "stats --mode raw-data" figures. TotalSize is
// the size of referenced data blobs, NOT filesystem disk usage, including
// repository metadata, free space or unreferenced blobs.
type vaulticStorageStatistics struct {
    TotalSize      uint64 `json:"total_size"`
    TotalBlobCount uint64 `json:"total_blob_count"`
    SnapshotsCount int    `json:"snapshots_count"`
}

func vaulticCollectStorageStatistics(ctx context.Context, options global.Options) (vaulticStorageStatistics, error) {
    var result vaulticStorageStatistics
    terminal, ok := options.Term.(*vaulticTerminal)
    if !ok { return result, errors.New("Terminal Vaultic requis pour les statistiques") }

    var output bytes.Buffer
    original := terminal.output
    terminal.output = &output
    defer func(){ terminal.output = original }()

    options.JSON = true
    if err:=runStats(ctx, StatsOptions{countMode:countModeRawData}, options,nil,terminal); err!=nil {
        return result,fmt.Errorf("Analyse de l'espace : %w",err)
    }
    if output.Len() > 1<<20 { return result, errors.New("Réponse de statistiques trop volumineuse") }
    decoder := json.NewDecoder(&output)
    if err:=decoder.Decode(&result); err!=nil { return result,fmt.Errorf("Statistiques illisibles : %w",err) }
    var extra json.RawMessage
    if err:=decoder.Decode(&extra);!errors.Is(err,io.EOF) {
        if err==nil { return result,errors.New("Statistiques supplémentaires inattendues") }
        return result,fmt.Errorf("Réponse de statistiques invalide : %w",err)
    }
    return result,nil
}
