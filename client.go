package transporter

// Code generated by api2. DO NOT EDIT.

import (
	"context"
	"net/url"

	"github.com/starius/api2"
)

type Client struct {
	api2client *api2.Client
}

var _ Service = (*Client)(nil)

func NewClient(baseURL string, opts ...api2.Option) (*Client, error) {
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, err
	}
	var routes []api2.Route

	routes = append(routes, GetRoutes(nil)...)

	api2client := api2.NewClient(routes, baseURL, opts...)
	return &Client{
		api2client: api2client,
	}, nil
}

func (c *Client) Close() error {
	return c.api2client.Close()
}

func (c *Client) PreminedList(ctx context.Context, req *PreminedListRequest) (res *PreminedListResponse, err error) {
	res = &PreminedListResponse{}
	err = c.api2client.Call(ctx, res, req)
	if err != nil {
		return nil, err
	}
	return
}

func (c *Client) CheckSolanaAddress(ctx context.Context, req *CheckSolanaAddressRequest) (res *CheckSolanaAddressResponse, err error) {
	res = &CheckSolanaAddressResponse{}
	err = c.api2client.Call(ctx, res, req)
	if err != nil {
		return nil, err
	}
	return
}

func (c *Client) CheckAllowance(ctx context.Context, req *CheckAllowanceRequest) (res *CheckAllowanceResponse, err error) {
	res = &CheckAllowanceResponse{}
	err = c.api2client.Call(ctx, res, req)
	if err != nil {
		return nil, err
	}
	return
}

func (c *Client) SubmitScpTx(ctx context.Context, req *SubmitScpTxRequest) (res *SubmitScpTxResponse, err error) {
	res = &SubmitScpTxResponse{}
	err = c.api2client.Call(ctx, res, req)
	if err != nil {
		return nil, err
	}
	return
}

func (c *Client) TransportStatus(ctx context.Context, req *TransportStatusRequest) (res *TransportStatusResponse, err error) {
	res = &TransportStatusResponse{}
	err = c.api2client.Call(ctx, res, req)
	if err != nil {
		return nil, err
	}
	return
}

func (c *Client) History(ctx context.Context, req *HistoryRequest) (res *HistoryResponse, err error) {
	res = &HistoryResponse{}
	err = c.api2client.Call(ctx, res, req)
	if err != nil {
		return nil, err
	}
	return
}
