// Copyright 2015 The go-BHEereum Authors
// This file is part of the go-BHEereum library.
//
// The go-BHEereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-BHEereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-BHEereum library. If not, see <http://www.gnu.org/licenses/>.

package BHE

import (
	"context"
	"errors"
	"math/big"
)

// BHEAPIBackend implements BHEapi.Backend for full nodes
type BHEAPIBackend struct {
	extRPCEnabled bool
	BHE           *BHEereum
	gpo           *gasprice.Oracle
}

// ChainConfig returns the active chain configuration.
func (b *BHEAPIBackend) ChainConfig() *params.ChainConfig {
	return b.BHE.blockchain.Config()
}

func (b *BHEAPIBackend) CurrentBlock() *types.Block {
	return b.BHE.blockchain.CurrentBlock()
}

func (b *BHEAPIBackend) SBHEead(number uint64) {
	b.BHE.protocolManager.downloader.Cancel()
	b.BHE.blockchain.SBHEead(number)
}

func (b *BHEAPIBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	// Pending block is only known by the miner
	if number == rpc.PendingBlockNumber {
		block := b.BHE.miner.PendingBlock()
		return block.Header(), nil
	}
	// Otherwise resolve and return the block
	if number == rpc.LatestBlockNumber {
		return b.BHE.blockchain.CurrentBlock().Header(), nil
	}
	return b.BHE.blockchain.GBHEeaderByNumber(uint64(number)), nil
}

func (b *BHEAPIBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.HeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.BHE.blockchain.GBHEeaderByHash(hash)
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.BHE.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		return header, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *BHEAPIBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return b.BHE.blockchain.GBHEeaderByHash(hash), nil
}

func (b *BHEAPIBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	// Pending block is only known by the miner
	if number == rpc.PendingBlockNumber {
		block := b.BHE.miner.PendingBlock()
		return block, nil
	}
	// Otherwise resolve and return the block
	if number == rpc.LatestBlockNumber {
		return b.BHE.blockchain.CurrentBlock(), nil
	}
	return b.BHE.blockchain.GetBlockByNumber(uint64(number)), nil
}

func (b *BHEAPIBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return b.BHE.blockchain.GetBlockByHash(hash), nil
}

func (b *BHEAPIBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.BlockByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.BHE.blockchain.GBHEeaderByHash(hash)
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.BHE.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		block := b.BHE.blockchain.GetBlock(hash, header.Number.Uint64())
		if block == nil {
			return nil, errors.New("header found, but block body is missing")
		}
		return block, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *BHEAPIBackend) StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	// Pending state is only known by the miner
	if number == rpc.PendingBlockNumber {
		block, state := b.BHE.miner.Pending()
		return state, block.Header(), nil
	}
	// Otherwise resolve the block number and return its state
	header, err := b.HeaderByNumber(ctx, number)
	if err != nil {
		return nil, nil, err
	}
	if header == nil {
		return nil, nil, errors.New("header not found")
	}
	stateDb, err := b.BHE.BlockChain().StateAt(header.Root)
	return stateDb, header, err
}

func (b *BHEAPIBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.StateAndHeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header, err := b.HeaderByHash(ctx, hash)
		if err != nil {
			return nil, nil, err
		}
		if header == nil {
			return nil, nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.BHE.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, nil, errors.New("hash is not currently canonical")
		}
		stateDb, err := b.BHE.BlockChain().StateAt(header.Root)
		return stateDb, header, err
	}
	return nil, nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *BHEAPIBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	return b.BHE.blockchain.GetReceiptsByHash(hash), nil
}

func (b *BHEAPIBackend) GetLogs(ctx context.Context, hash common.Hash) ([][]*types.Log, error) {
	receipts := b.BHE.blockchain.GetReceiptsByHash(hash)
	if receipts == nil {
		return nil, nil
	}
	logs := make([][]*types.Log, len(receipts))
	for i, receipt := range receipts {
		logs[i] = receipt.Logs
	}
	return logs, nil
}

func (b *BHEAPIBackend) GetTd(blockHash common.Hash) *big.Int {
	return b.BHE.blockchain.GetTdByHash(blockHash)
}

func (b *BHEAPIBackend) GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header) (*vm.EVM, func() error, error) {
	vmError := func() error { return nil }

	context := core.NewEVMContext(msg, header, b.BHE.BlockChain(), nil)
	return vm.NewEVM(context, state, b.BHE.blockchain.Config(), *b.BHE.blockchain.GetVMConfig()), vmError, nil
}

func (b *BHEAPIBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.BHE.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *BHEAPIBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.BHE.miner.SubscribePendingLogs(ch)
}

func (b *BHEAPIBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.BHE.BlockChain().SubscribeChainEvent(ch)
}

func (b *BHEAPIBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.BHE.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *BHEAPIBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.BHE.BlockChain().SubscribeChainSideEvent(ch)
}

func (b *BHEAPIBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.BHE.BlockChain().SubscribeLogsEvent(ch)
}

func (b *BHEAPIBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return b.BHE.txPool.AddLocal(signedTx)
}

func (b *BHEAPIBackend) GetPoolTransactions() (types.Transactions, error) {
	pending, err := b.BHE.txPool.Pending()
	if err != nil {
		return nil, err
	}
	var txs types.Transactions
	for _, batch := range pending {
		txs = append(txs, batch...)
	}
	return txs, nil
}

func (b *BHEAPIBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.BHE.txPool.Get(hash)
}

func (b *BHEAPIBackend) GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	tx, blockHash, blockNumber, index := rawdb.ReadTransaction(b.BHE.ChainDb(), txHash)
	return tx, blockHash, blockNumber, index, nil
}

func (b *BHEAPIBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.BHE.txPool.Nonce(addr), nil
}

func (b *BHEAPIBackend) Stats() (pending int, queued int) {
	return b.BHE.txPool.Stats()
}

func (b *BHEAPIBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.BHE.TxPool().Content()
}

func (b *BHEAPIBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.BHE.TxPool().SubscribeNewTxsEvent(ch)
}

func (b *BHEAPIBackend) Downloader() *downloader.Downloader {
	return b.BHE.Downloader()
}

func (b *BHEAPIBackend) ProtocolVersion() int {
	return b.BHE.BHEVersion()
}

func (b *BHEAPIBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestPrice(ctx)
}

func (b *BHEAPIBackend) ChainDb() BHEdb.Database {
	return b.BHE.ChainDb()
}

func (b *BHEAPIBackend) EventMux() *event.TypeMux {
	return b.BHE.EventMux()
}

func (b *BHEAPIBackend) AccountManager() *accounts.Manager {
	return b.BHE.AccountManager()
}

func (b *BHEAPIBackend) ExtRPCEnabled() bool {
	return b.extRPCEnabled
}

func (b *BHEAPIBackend) RPCGasCap() *big.Int {
	return b.BHE.config.RPCGasCap
}

func (b *BHEAPIBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.BHE.bloomIndexer.Sections()
	return params.BloomBitsBlocks, sections
}

func (b *BHEAPIBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.BHE.bloomRequests)
	}
}
