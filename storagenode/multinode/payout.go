// Copyright (C) 2021 Storj Labs, Inc.
// See LICENSE for copying information.

package multinode

import (
	"context"
	"time"

	"go.uber.org/zap"

	"storj.io/common/rpc/rpcstatus"
	"storj.io/storj/private/multinodepb"
	"storj.io/storj/storagenode/apikeys"
	"storj.io/storj/storagenode/payouts"
	"storj.io/storj/storagenode/payouts/estimatedpayouts"
)

var _ multinodepb.DRPCPayoutServer = (*PayoutEndpoint)(nil)

// PayoutEndpoint implements multinode payouts endpoint.
//
// architecture: Endpoint
type PayoutEndpoint struct {
	multinodepb.DRPCPayoutUnimplementedServer

	log              *zap.Logger
	apiKeys          *apikeys.Service
	estimatedPayouts *estimatedpayouts.Service
	db               payouts.DB
}

// NewPayoutEndpoint creates new multinode payouts endpoint.
func NewPayoutEndpoint(log *zap.Logger, apiKeys *apikeys.Service, estimatedPayouts *estimatedpayouts.Service, db payouts.DB) *PayoutEndpoint {
	return &PayoutEndpoint{
		log:              log,
		apiKeys:          apiKeys,
		estimatedPayouts: estimatedPayouts,
		db:               db,
	}
}

// Earned returns total earned amount.
func (payout *PayoutEndpoint) Earned(ctx context.Context, req *multinodepb.EarnedRequest) (_ *multinodepb.EarnedResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = authenticate(ctx, payout.apiKeys, req.GetHeader()); err != nil {
		return nil, rpcstatus.Wrap(rpcstatus.Unauthenticated, err)
	}

	earned, err := payout.db.GetTotalEarned(ctx)
	if err != nil {
		return nil, rpcstatus.Wrap(rpcstatus.Internal, err)
	}

	return &multinodepb.EarnedResponse{
		Total: earned,
	}, nil
}

// EarnedPerSatellite returns total earned amount per satellite.
func (payout *PayoutEndpoint) EarnedPerSatellite(ctx context.Context, req *multinodepb.EarnedPerSatelliteRequest) (_ *multinodepb.EarnedPerSatelliteResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = authenticate(ctx, payout.apiKeys, req.GetHeader()); err != nil {
		return nil, rpcstatus.Wrap(rpcstatus.Unauthenticated, err)
	}

	var resp multinodepb.EarnedPerSatelliteResponse
	satelliteIDs, err := payout.db.GetPayingSatellitesIDs(ctx)
	if err != nil {
		return nil, rpcstatus.Wrap(rpcstatus.Internal, err)
	}

	for i := 0; i < len(satelliteIDs); i++ {
		earned, err := payout.db.GetEarnedAtSatellite(ctx, satelliteIDs[i])
		if err != nil {
			return nil, rpcstatus.Wrap(rpcstatus.Internal, err)
		}

		resp.EarnedSatellite = append(resp.EarnedSatellite, &multinodepb.EarnedSatellite{
			Total:       earned,
			SatelliteId: satelliteIDs[i],
		})
	}

	return &resp, nil
}

// EstimatedPayoutTotal returns estimated earnings for current month from all satellites.
func (payout *PayoutEndpoint) EstimatedPayoutTotal(ctx context.Context, req *multinodepb.EstimatedPayoutTotalRequest) (_ *multinodepb.EstimatedPayoutTotalResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = authenticate(ctx, payout.apiKeys, req.GetHeader()); err != nil {
		return nil, rpcstatus.Wrap(rpcstatus.Unauthenticated, err)
	}

	estimated, err := payout.estimatedPayouts.GetAllSatellitesEstimatedPayout(ctx, time.Now())
	if err != nil {
		return &multinodepb.EstimatedPayoutTotalResponse{}, rpcstatus.Wrap(rpcstatus.Internal, err)
	}

	return &multinodepb.EstimatedPayoutTotalResponse{EstimatedEarnings: estimated.CurrentMonthExpectations}, nil
}

// EstimatedPayoutSatellite returns estimated earnings for current month from specific satellite.
func (payout *PayoutEndpoint) EstimatedPayoutSatellite(ctx context.Context, req *multinodepb.EstimatedPayoutSatelliteRequest) (_ *multinodepb.EstimatedPayoutSatelliteResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = authenticate(ctx, payout.apiKeys, req.GetHeader()); err != nil {
		return nil, rpcstatus.Wrap(rpcstatus.Unauthenticated, err)
	}

	estimated, err := payout.estimatedPayouts.GetSatelliteEstimatedPayout(ctx, req.SatelliteId, time.Now())
	if err != nil {
		return &multinodepb.EstimatedPayoutSatelliteResponse{}, rpcstatus.Wrap(rpcstatus.Internal, err)
	}

	return &multinodepb.EstimatedPayoutSatelliteResponse{EstimatedEarnings: estimated.CurrentMonthExpectations}, nil
}

// AllSatellitesSummary returns all satellites all time payout summary.
func (payout *PayoutEndpoint) AllSatellitesSummary(ctx context.Context, req *multinodepb.AllSatellitesSummaryRequest) (_ *multinodepb.AllSatellitesSummaryResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = authenticate(ctx, payout.apiKeys, req.GetHeader()); err != nil {
		return nil, rpcstatus.Wrap(rpcstatus.Unauthenticated, err)
	}

	var totalPaid, totalHeld int64
	satelliteIDs, err := payout.db.GetPayingSatellitesIDs(ctx)
	if err != nil {
		return &multinodepb.AllSatellitesSummaryResponse{}, rpcstatus.Wrap(rpcstatus.Internal, err)
	}

	for _, id := range satelliteIDs {
		paid, held, err := payout.db.GetSatelliteSummary(ctx, id)
		if err != nil {
			return &multinodepb.AllSatellitesSummaryResponse{}, rpcstatus.Wrap(rpcstatus.Internal, err)
		}

		totalHeld += held
		totalPaid += paid
	}

	return &multinodepb.AllSatellitesSummaryResponse{PayoutInfo: &multinodepb.PayoutInfo{Paid: totalPaid, Held: totalHeld}}, nil
}

// AllSatellitesPeriodSummary returns all satellites period payout summary.
func (payout *PayoutEndpoint) AllSatellitesPeriodSummary(ctx context.Context, req *multinodepb.AllSatellitesPeriodSummaryRequest) (_ *multinodepb.AllSatellitesPeriodSummaryResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = authenticate(ctx, payout.apiKeys, req.GetHeader()); err != nil {
		return nil, rpcstatus.Wrap(rpcstatus.Unauthenticated, err)
	}

	var totalPaid, totalHeld int64
	satelliteIDs, err := payout.db.GetPayingSatellitesIDs(ctx)
	if err != nil {
		return &multinodepb.AllSatellitesPeriodSummaryResponse{}, rpcstatus.Wrap(rpcstatus.Internal, err)
	}

	for _, id := range satelliteIDs {
		paid, held, err := payout.db.GetSatellitePeriodSummary(ctx, id, req.Period)
		if err != nil {
			return &multinodepb.AllSatellitesPeriodSummaryResponse{}, rpcstatus.Wrap(rpcstatus.Internal, err)
		}

		totalHeld += held
		totalPaid += paid
	}

	return &multinodepb.AllSatellitesPeriodSummaryResponse{PayoutInfo: &multinodepb.PayoutInfo{Held: totalHeld, Paid: totalPaid}}, nil
}

// SatelliteSummary returns satellite all time payout summary.
func (payout *PayoutEndpoint) SatelliteSummary(ctx context.Context, req *multinodepb.SatelliteSummaryRequest) (_ *multinodepb.SatelliteSummaryResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = authenticate(ctx, payout.apiKeys, req.GetHeader()); err != nil {
		return nil, rpcstatus.Wrap(rpcstatus.Unauthenticated, err)
	}

	var totalPaid, totalHeld int64

	totalPaid, totalHeld, err = payout.db.GetSatelliteSummary(ctx, req.SatelliteId)
	if err != nil {
		return &multinodepb.SatelliteSummaryResponse{}, rpcstatus.Wrap(rpcstatus.Internal, err)
	}

	return &multinodepb.SatelliteSummaryResponse{PayoutInfo: &multinodepb.PayoutInfo{Held: totalHeld, Paid: totalPaid}}, nil
}

// SatellitePeriodSummary returns satellite period payout summary.
func (payout *PayoutEndpoint) SatellitePeriodSummary(ctx context.Context, req *multinodepb.SatellitePeriodSummaryRequest) (_ *multinodepb.SatellitePeriodSummaryResponse, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = authenticate(ctx, payout.apiKeys, req.GetHeader()); err != nil {
		return nil, rpcstatus.Wrap(rpcstatus.Unauthenticated, err)
	}

	var totalPaid, totalHeld int64

	totalPaid, totalHeld, err = payout.db.GetSatellitePeriodSummary(ctx, req.SatelliteId, req.Period)
	if err != nil {
		return &multinodepb.SatellitePeriodSummaryResponse{}, rpcstatus.Wrap(rpcstatus.Internal, err)
	}

	return &multinodepb.SatellitePeriodSummaryResponse{PayoutInfo: &multinodepb.PayoutInfo{Held: totalHeld, Paid: totalPaid}}, nil
}
