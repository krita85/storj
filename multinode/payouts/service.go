// Copyright (C) 2021 Storj Labs, Inc.
// See LICENSE for copying information.

package payouts

import (
	"context"

	"github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/common/rpc"
	"storj.io/common/storj"
	"storj.io/storj/multinode/nodes"
	"storj.io/storj/private/multinodepb"
)

var (
	mon = monkit.Package()
	// Error is an error class for payouts service error.
	Error = errs.Class("payouts")
)

// Service exposes all payouts related logic.
//
// architecture: Service
type Service struct {
	log    *zap.Logger
	dialer rpc.Dialer
	nodes  nodes.DB
}

// NewService creates new instance of Service.
func NewService(log *zap.Logger, dialer rpc.Dialer, nodes nodes.DB) *Service {
	return &Service{
		log:    log,
		dialer: dialer,
		nodes:  nodes,
	}
}

// GetAllNodesAllTimeEarned retrieves all nodes earned amount for all time.
func (service *Service) GetAllNodesAllTimeEarned(ctx context.Context) (earned int64, err error) {
	defer mon.Task()(&ctx)(&err)

	storageNodes, err := service.nodes.List(ctx)
	if err != nil {
		return 0, Error.Wrap(err)
	}

	for _, node := range storageNodes {
		amount, err := service.getAmount(ctx, node)
		if err != nil {
			service.log.Error("failed to getAmount", zap.Error(err))
			continue
		}

		earned += amount
	}

	return earned, nil
}

// GetAllNodesEarnedOnSatellite retrieves all nodes earned amount for all time per satellite.
func (service *Service) GetAllNodesEarnedOnSatellite(ctx context.Context) (earned []SatelliteSummary, err error) {
	defer mon.Task()(&ctx)(&err)

	storageNodes, err := service.nodes.List(ctx)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	var listSatellites storj.NodeIDList
	var listNodesEarnedPerSatellite []multinodepb.EarnedPerSatelliteResponse

	for _, node := range storageNodes {
		earnedPerSatellite, err := service.getEarnedOnSatellite(ctx, node)
		if err != nil {
			service.log.Error("failed to getEarnedFromSatellite", zap.Error(err))
			continue
		}

		listNodesEarnedPerSatellite = append(listNodesEarnedPerSatellite, earnedPerSatellite)
		for i := 0; i < len(earnedPerSatellite.EarnedSatellite); i++ {
			listSatellites = append(listSatellites, earnedPerSatellite.EarnedSatellite[i].SatelliteId)
		}
	}

	if listSatellites == nil {
		return []SatelliteSummary{}, nil
	}

	uniqueSatelliteIDs := listSatellites.Unique()
	for t := 0; t < len(uniqueSatelliteIDs); t++ {
		earned = append(earned, SatelliteSummary{
			SatelliteID: uniqueSatelliteIDs[t],
		})
	}

	for i := 0; i < len(listNodesEarnedPerSatellite); i++ {
		singleNodeEarnedPerSatellite := listNodesEarnedPerSatellite[i].EarnedSatellite
		for j := 0; j < len(singleNodeEarnedPerSatellite); j++ {
			for k := 0; k < len(earned); k++ {
				if singleNodeEarnedPerSatellite[j].SatelliteId == earned[k].SatelliteID {
					earned[k].Earned += singleNodeEarnedPerSatellite[j].Total
				}
			}
		}
	}

	return earned, nil
}

// NodesSummary returns all satellites all time stats.
func (service *Service) NodesSummary(ctx context.Context) (_ Summary, err error) {
	defer mon.Task()(&ctx)(&err)

	var summary Summary

	list, err := service.nodes.List(ctx)
	if err != nil {
		return Summary{}, Error.Wrap(err)
	}

	for _, node := range list {
		info, err := service.getAllSatellitesAllTime(ctx, node)
		if err != nil {
			return Summary{}, Error.Wrap(err)
		}

		summary.Add(info.Held, info.Paid, node.ID, node.Name)
	}

	return summary, nil
}

// NodesPeriodSummary returns all satellites stats for specific period.
func (service *Service) NodesPeriodSummary(ctx context.Context, period string) (_ Summary, err error) {
	defer mon.Task()(&ctx)(&err)

	var summary Summary

	list, err := service.nodes.List(ctx)
	if err != nil {
		return Summary{}, Error.Wrap(err)
	}

	for _, node := range list {
		info, err := service.getAllSatellitesPeriod(ctx, node, period)
		if err != nil {
			return Summary{}, Error.Wrap(err)
		}

		summary.Add(info.Held, info.Paid, node.ID, node.Name)
	}

	return summary, nil
}

// NodesSatelliteSummary returns specific satellite all time stats.
func (service *Service) NodesSatelliteSummary(ctx context.Context, satelliteID storj.NodeID) (_ Summary, err error) {
	defer mon.Task()(&ctx)(&err)
	var summary Summary

	list, err := service.nodes.List(ctx)
	if err != nil {
		return Summary{}, Error.Wrap(err)
	}

	for _, node := range list {
		info, err := service.nodeSatelliteSummary(ctx, node, satelliteID)
		if err != nil {
			return Summary{}, Error.Wrap(err)
		}

		summary.Add(info.Held, info.Paid, node.ID, node.Name)
	}

	return summary, nil
}

// NodesSatellitePeriodSummary returns specific satellite stats for specific period.
func (service *Service) NodesSatellitePeriodSummary(ctx context.Context, satelliteID storj.NodeID, period string) (_ Summary, err error) {
	defer mon.Task()(&ctx)(&err)
	var summary Summary

	list, err := service.nodes.List(ctx)
	if err != nil {
		return Summary{}, Error.Wrap(err)
	}

	for _, node := range list {
		info, err := service.nodeSatellitePeriodSummary(ctx, node, satelliteID, period)
		if err != nil {
			return Summary{}, Error.Wrap(err)
		}

		summary.Add(info.Held, info.Paid, node.ID, node.Name)
	}

	return summary, nil
}

// nodeSatelliteSummary returns payout info for single satellite, for specific node.
func (service *Service) nodeSatelliteSummary(ctx context.Context, node nodes.Node, satelliteID storj.NodeID) (info *multinodepb.PayoutInfo, err error) {
	conn, err := service.dialer.DialNodeURL(ctx, storj.NodeURL{
		ID:      node.ID,
		Address: node.PublicAddress,
	})
	if err != nil {
		return &multinodepb.PayoutInfo{}, Error.Wrap(err)
	}

	defer func() {
		err = errs.Combine(err, conn.Close())
	}()

	payoutClient := multinodepb.NewDRPCPayoutClient(conn)
	header := &multinodepb.RequestHeader{
		ApiKey: node.APISecret,
	}

	response, err := payoutClient.SatelliteSummary(ctx, &multinodepb.SatelliteSummaryRequest{Header: header, SatelliteId: satelliteID})
	if err != nil {
		return &multinodepb.PayoutInfo{}, Error.Wrap(err)
	}

	return response.PayoutInfo, nil
}

// nodeSatellitePeriodSummary returns satellite payout info for specific node for specific period.
func (service *Service) nodeSatellitePeriodSummary(ctx context.Context, node nodes.Node, satelliteID storj.NodeID, period string) (info *multinodepb.PayoutInfo, err error) {
	conn, err := service.dialer.DialNodeURL(ctx, storj.NodeURL{
		ID:      node.ID,
		Address: node.PublicAddress,
	})
	if err != nil {
		return &multinodepb.PayoutInfo{}, Error.Wrap(err)
	}

	defer func() {
		err = errs.Combine(err, conn.Close())
	}()

	payoutClient := multinodepb.NewDRPCPayoutClient(conn)
	header := &multinodepb.RequestHeader{
		ApiKey: node.APISecret,
	}

	response, err := payoutClient.SatellitePeriodSummary(ctx, &multinodepb.SatellitePeriodSummaryRequest{Header: header, SatelliteId: satelliteID, Period: period})
	if err != nil {
		return &multinodepb.PayoutInfo{}, Error.Wrap(err)
	}

	return response.PayoutInfo, nil
}

func (service *Service) getAllSatellitesPeriod(ctx context.Context, node nodes.Node, period string) (info *multinodepb.PayoutInfo, err error) {
	conn, err := service.dialer.DialNodeURL(ctx, storj.NodeURL{
		ID:      node.ID,
		Address: node.PublicAddress,
	})
	if err != nil {
		return &multinodepb.PayoutInfo{}, Error.Wrap(err)
	}

	defer func() {
		err = errs.Combine(err, conn.Close())
	}()

	payoutClient := multinodepb.NewDRPCPayoutClient(conn)
	header := &multinodepb.RequestHeader{
		ApiKey: node.APISecret,
	}

	response, err := payoutClient.AllSatellitesPeriodSummary(ctx, &multinodepb.AllSatellitesPeriodSummaryRequest{Header: header, Period: period})
	if err != nil {
		return &multinodepb.PayoutInfo{}, Error.Wrap(err)
	}

	return response.PayoutInfo, nil
}

func (service *Service) getAllSatellitesAllTime(ctx context.Context, node nodes.Node) (info *multinodepb.PayoutInfo, err error) {
	conn, err := service.dialer.DialNodeURL(ctx, storj.NodeURL{
		ID:      node.ID,
		Address: node.PublicAddress,
	})
	if err != nil {
		return &multinodepb.PayoutInfo{}, Error.Wrap(err)
	}

	defer func() {
		err = errs.Combine(err, conn.Close())
	}()

	payoutClient := multinodepb.NewDRPCPayoutClient(conn)
	header := &multinodepb.RequestHeader{
		ApiKey: node.APISecret,
	}

	response, err := payoutClient.AllSatellitesSummary(ctx, &multinodepb.AllSatellitesSummaryRequest{Header: header})
	if err != nil {
		return &multinodepb.PayoutInfo{}, Error.Wrap(err)
	}

	return response.PayoutInfo, nil
}

// NodesSatelliteEstimations returns specific satellite all time estimated earnings.
func (service *Service) NodesSatelliteEstimations(ctx context.Context, satelliteID storj.NodeID) (_ int64, err error) {
	defer mon.Task()(&ctx)(&err)

	var estimatedEarnings int64

	list, err := service.nodes.List(ctx)
	if err != nil {
		return 0, Error.Wrap(err)
	}

	for _, node := range list {
		estimation, err := service.nodeSatelliteEstimations(ctx, node, satelliteID)
		if err != nil {
			return 0, Error.Wrap(err)
		}

		estimatedEarnings += estimation
	}

	return estimatedEarnings, nil
}

// NodesEstimations returns all satellites all time estimated earnings.
func (service *Service) NodesEstimations(ctx context.Context) (_ int64, err error) {
	defer mon.Task()(&ctx)(&err)

	var estimatedEarnings int64

	list, err := service.nodes.List(ctx)
	if err != nil {
		return 0, Error.Wrap(err)
	}

	for _, node := range list {
		estimation, err := service.nodeEstimations(ctx, node)
		if err != nil {
			return 0, Error.Wrap(err)
		}

		estimatedEarnings += estimation
	}

	return estimatedEarnings, nil
}

// nodeEstimations retrieves data from a single node.
func (service *Service) nodeEstimations(ctx context.Context, node nodes.Node) (estimation int64, err error) {
	conn, err := service.dialer.DialNodeURL(ctx, storj.NodeURL{
		ID:      node.ID,
		Address: node.PublicAddress,
	})
	if err != nil {
		return 0, Error.Wrap(err)
	}

	defer func() {
		err = errs.Combine(err, conn.Close())
	}()

	payoutClient := multinodepb.NewDRPCPayoutClient(conn)
	header := &multinodepb.RequestHeader{
		ApiKey: node.APISecret,
	}

	response, err := payoutClient.EstimatedPayoutTotal(ctx, &multinodepb.EstimatedPayoutTotalRequest{Header: header})
	if err != nil {
		return 0, Error.Wrap(err)
	}

	return response.EstimatedEarnings, nil
}

// nodeSatelliteEstimations retrieves data from a single node.
func (service *Service) nodeSatelliteEstimations(ctx context.Context, node nodes.Node, satelliteID storj.NodeID) (estimation int64, err error) {
	conn, err := service.dialer.DialNodeURL(ctx, storj.NodeURL{
		ID:      node.ID,
		Address: node.PublicAddress,
	})
	if err != nil {
		return 0, Error.Wrap(err)
	}

	defer func() {
		err = errs.Combine(err, conn.Close())
	}()
	payoutClient := multinodepb.NewDRPCPayoutClient(conn)
	header := &multinodepb.RequestHeader{
		ApiKey: node.APISecret,
	}
	response, err := payoutClient.EstimatedPayoutSatellite(ctx, &multinodepb.EstimatedPayoutSatelliteRequest{Header: header, SatelliteId: satelliteID})
	if err != nil {
		return 0, Error.Wrap(err)
	}
	return response.EstimatedEarnings, nil
}

func (service *Service) getAmount(ctx context.Context, node nodes.Node) (_ int64, err error) {
	conn, err := service.dialer.DialNodeURL(ctx, storj.NodeURL{
		ID:      node.ID,
		Address: node.PublicAddress,
	})
	if err != nil {
		return 0, Error.Wrap(err)
	}

	defer func() {
		err = errs.Combine(err, conn.Close())
	}()

	payoutClient := multinodepb.NewDRPCPayoutClient(conn)
	header := &multinodepb.RequestHeader{
		ApiKey: node.APISecret,
	}

	amount, err := payoutClient.Earned(ctx, &multinodepb.EarnedRequest{Header: header})
	if err != nil {
		return 0, Error.Wrap(err)
	}

	return amount.Total, nil
}

func (service *Service) getEarnedOnSatellite(ctx context.Context, node nodes.Node) (_ multinodepb.EarnedPerSatelliteResponse, err error) {
	conn, err := service.dialer.DialNodeURL(ctx, storj.NodeURL{
		ID:      node.ID,
		Address: node.PublicAddress,
	})
	if err != nil {
		return multinodepb.EarnedPerSatelliteResponse{}, Error.Wrap(err)
	}

	defer func() {
		err = errs.Combine(err, conn.Close())
	}()

	payoutClient := multinodepb.NewDRPCPayoutClient(conn)
	header := &multinodepb.RequestHeader{
		ApiKey: node.APISecret,
	}

	response, err := payoutClient.EarnedPerSatellite(ctx, &multinodepb.EarnedPerSatelliteRequest{Header: header})
	if err != nil {
		return multinodepb.EarnedPerSatelliteResponse{}, Error.Wrap(err)
	}

	return *response, nil
}
