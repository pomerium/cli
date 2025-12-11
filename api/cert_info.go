package api

import (
	"fmt"

	"github.com/golang/groupcache/lru"
	"google.golang.org/protobuf/proto"

	pb "github.com/pomerium/cli/proto"
	"github.com/pomerium/pomerium/pkg/cryptutil"
)

func withCertInfo(cache *lru.Cache, records []*pb.Record) []*pb.Record {
	for _, r := range records {
		if r.Conn == nil || r.Conn.ClientCert == nil {
			continue
		}
		cert := r.Conn.ClientCert
		var err error
		cert.Info, err = getCertInfo(cache, cert.Cert)
		if err != nil {
			cert.Info = certInfoError(err.Error())
		}
	}
	return records
}

func certInfoError(message string) *pb.CertificateInfo {
	return &pb.CertificateInfo{Error: proto.String(message)}
}

func getCertInfo(cache *lru.Cache, raw []byte) (*pb.CertificateInfo, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("missing cert data")
	}

	key := string(raw)
	cached, ok := cache.Get(key)
	if ok {
		info, ok := cached.(*pb.CertificateInfo)
		if !ok {
			return nil, fmt.Errorf("invalid cached entry type")
		}
		return info, nil
	}

	parsed, err := cryptutil.ParsePEMCertificate(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing cert: %w", err)
	}

	info := pb.NewCertInfo(parsed)
	cache.Add(key, info)
	return info, nil
}
