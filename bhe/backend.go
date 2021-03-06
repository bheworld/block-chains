// Copyright 2014 The go-BHEereum Authors
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

// Package BHE implements the BHEereum protocol.
package BHE

import (
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"
)

type LesServer interface {
	Start(srvr *p2p.Server)
	Stop()
	APIs() []rpc.API
	Protocols() []p2p.Protocol
	SetBloomBitsIndexer(bbIndexer *core.ChainIndexer)
	SetContractBackend(bind.ContractBackend)
}

// BHEereum implements the BHEereum full node service.
type BHEereum struct {
	config *Config

	// Handlers
	txPool          *core.TxPool
	blockchain      *core.BlockChain
	protocolManager *ProtocolManager
	lesServer       LesServer
	dialCandidates  enode.Iterator

	// DB interfaces
	chainDb BHEdb.Database // Block chain database

	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager

	bloomRequests     chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer      *core.ChainIndexer             // Bloom indexer operating during block imports
	closeBloomHandler chan struct{}

	APIBackend *BHEAPIBackend

	miner     *miner.Miner
	gasPrice  *big.Int
	BHEerbase common.Address

	networkID     uint64
	netRPCService *BHEapi.PublicNetAPI

	lock sync.RWMutex // Protects the variadic fields (e.g. gas price and BHEerbase)
}

func (s *BHEereum) AddLesServer(ls LesServer) {
	s.lesServer = ls
	ls.SetBloomBitsIndexer(s.bloomIndexer)
}

// SetClient sets a rpc client which connecting to our local node.
func (s *BHEereum) SetContractBackend(backend bind.ContractBackend) {
	// Pass the rpc client to les server if it is enabled.
	if s.lesServer != nil {
		s.lesServer.SetContractBackend(backend)
	}
}

// New creates a new BHEereum object (including the
// initialisation of the common BHEereum object)
func New(ctx *node.ServiceContext, config *Config) (*BHEereum, error) {
	// Ensure configuration values are compatible and sane
	if config.SyncMode == downloader.LightSync {
		return nil, errors.New("can't run BHE.BHEereum in light sync mode, use les.LightBHEereum")
	}
	if !config.SyncMode.IsValid() {
		return nil, fmt.Errorf("invalid sync mode %d", config.SyncMode)
	}
	if config.Miner.GasPrice == nil || config.Miner.GasPrice.Cmp(common.Big0) <= 0 {
		log.Warn("Sanitizing invalid miner gas price", "provided", config.Miner.GasPrice, "updated", DefaultConfig.Miner.GasPrice)
		config.Miner.GasPrice = new(big.Int).Set(DefaultConfig.Miner.GasPrice)
	}
	if config.NoPruning && config.TrieDirtyCache > 0 {
		if config.SnapshotCache > 0 {
			config.TrieCleanCache += config.TrieDirtyCache * 3 / 5
			config.SnapshotCache += config.TrieDirtyCache * 2 / 5
		} else {
			config.TrieCleanCache += config.TrieDirtyCache
		}
		config.TrieDirtyCache = 0
	}
	log.Info("Allocated trie memory caches", "clean", common.StorageSize(config.TrieCleanCache)*1024*1024, "dirty", common.StorageSize(config.TrieDirtyCache)*1024*1024)

	// Assemble the BHEereum object
	chainDb, err := ctx.OpenDatabaseWithFreezer("chaindata", config.DatabaseCache, config.DatabaseHandles, config.DatabaseFreezer, "BHE/db/chaindata/")
	if err != nil {
		return nil, err
	}
	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlock(chainDb, config.Genesis)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		return nil, genesisErr
	}
	log.Info("Initialised chain configuration", "config", chainConfig)

	BHE := &BHEereum{
		config:            config,
		chainDb:           chainDb,
		eventMux:          ctx.EventMux,
		accountManager:    ctx.AccountManager,
		engine:            CreateConsensusEngine(ctx, chainConfig, &config.BHEash, config.Miner.Notify, config.Miner.Noverify, chainDb),
		closeBloomHandler: make(chan struct{}),
		networkID:         config.NetworkId,
		gasPrice:          config.Miner.GasPrice,
		BHEerbase:         config.Miner.BHEerbase,
		bloomRequests:     make(chan chan *bloombits.Retrieval),
		bloomIndexer:      NewBloomIndexer(chainDb, params.BloomBitsBlocks, params.BloomConfirms),
	}

	bcVersion := rawdb.ReadDatabaseVersion(chainDb)
	var dbVer = "<nil>"
	if bcVersion != nil {
		dbVer = fmt.Sprintf("%d", *bcVersion)
	}
	log.Info("Initialising BHEereum protocol", "versions", ProtocolVersions, "network", config.NetworkId, "dbversion", dbVer)

	if !config.SkipBcVersionCheck {
		if bcVersion != nil && *bcVersion > core.BlockChainVersion {
			return nil, fmt.Errorf("database version is v%d, GBHE %s only supports v%d", *bcVersion, params.VersionWithMeta, core.BlockChainVersion)
		} else if bcVersion == nil || *bcVersion < core.BlockChainVersion {
			log.Warn("Upgrade blockchain database version", "from", dbVer, "to", core.BlockChainVersion)
			rawdb.WriteDatabaseVersion(chainDb, core.BlockChainVersion)
		}
	}
	var (
		vmConfig = vm.Config{
			EnablePreimageRecording: config.EnablePreimageRecording,
			EWASMInterpreter:        config.EWASMInterpreter,
			EVMInterpreter:          config.EVMInterpreter,
		}
		cacheConfig = &core.CacheConfig{
			TrieCleanLimit:      config.TrieCleanCache,
			TrieCleanNoPrefetch: config.NoPrefetch,
			TrieDirtyLimit:      config.TrieDirtyCache,
			TrieDirtyDisabled:   config.NoPruning,
			TrieTimeLimit:       config.TrieTimeout,
			SnapshotLimit:       config.SnapshotCache,
		}
	)
	BHE.blockchain, err = core.NewBlockChain(chainDb, cacheConfig, chainConfig, BHE.engine, vmConfig, BHE.shouldPreserve, &config.TxLookupLimit)
	if err != nil {
		return nil, err
	}
	// Rewind the chain in case of an incompatible config upgrade.
	if compat, ok := genesisErr.(*params.ConfigCompatError); ok {
		log.Warn("Rewinding chain to upgrade configuration", "err", compat)
		BHE.blockchain.SBHEead(compat.RewindTo)
		rawdb.WriteChainConfig(chainDb, genesisHash, chainConfig)
	}
	BHE.bloomIndexer.Start(BHE.blockchain)

	if config.TxPool.Journal != "" {
		config.TxPool.Journal = ctx.ResolvePath(config.TxPool.Journal)
	}
	BHE.txPool = core.NewTxPool(config.TxPool, chainConfig, BHE.blockchain)

	// Permit the downloader to use the trie cache allowance during fast sync
	cacheLimit := cacheConfig.TrieCleanLimit + cacheConfig.TrieDirtyLimit + cacheConfig.SnapshotLimit
	checkpoint := config.Checkpoint
	if checkpoint == nil {
		checkpoint = params.TrustedCheckpoints[genesisHash]
	}
	if BHE.protocolManager, err = NewProtocolManager(chainConfig, checkpoint, config.SyncMode, config.NetworkId, BHE.eventMux, BHE.txPool, BHE.engine, BHE.blockchain, chainDb, cacheLimit, config.Whitelist); err != nil {
		return nil, err
	}
	BHE.miner = miner.New(BHE, &config.Miner, chainConfig, BHE.EventMux(), BHE.engine, BHE.isLocalBlock)
	BHE.miner.SetExtra(makeExtraData(config.Miner.ExtraData))

	BHE.APIBackend = &BHEAPIBackend{ctx.ExtRPCEnabled(), BHE, nil}
	gpoParams := config.GPO
	if gpoParams.Default == nil {
		gpoParams.Default = config.Miner.GasPrice
	}
	BHE.APIBackend.gpo = gasprice.NewOracle(BHE.APIBackend, gpoParams)

	BHE.dialCandidates, err = BHE.setupDiscovery(&ctx.Config.P2P)
	if err != nil {
		return nil, err
	}

	return BHE, nil
}

func makeExtraData(extra []byte) []byte {
	if len(extra) == 0 {
		// create default extradata
		extra, _ = rlp.EncodeToBytes([]interface{}{
			uint(params.VersionMajor<<16 | params.VersionMinor<<8 | params.VersionPatch),
			"gBHE",
			runtime.Version(),
			runtime.GOOS,
		})
	}
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		log.Warn("Miner extra data exceed limit", "extra", hexutil.Bytes(extra), "limit", params.MaximumExtraDataSize)
		extra = nil
	}
	return extra
}

// CreateConsensusEngine creates the required type of consensus engine instance for an BHEereum service
func CreateConsensusEngine(ctx *node.ServiceContext, chainConfig *params.ChainConfig, config *BHEash.Config, notify []string, noverify bool, db BHEdb.Database) consensus.Engine {
	// If proof-of-authority is requested, set it up
	if chainConfig.Clique != nil {
		return clique.New(chainConfig.Clique, db)
	}
	// Otherwise assume proof-of-work
	switch config.PowMode {
	case BHEash.ModeFake:
		log.Warn("BHEash used in fake mode")
		return BHEash.NewFaker()
	case BHEash.ModeTest:
		log.Warn("BHEash used in test mode")
		return BHEash.NewTester(nil, noverify)
	case BHEash.ModeShared:
		log.Warn("BHEash used in shared mode")
		return BHEash.NewShared()
	default:
		engine := BHEash.New(BHEash.Config{
			CacheDir:         ctx.ResolvePath(config.CacheDir),
			CachesInMem:      config.CachesInMem,
			CachesOnDisk:     config.CachesOnDisk,
			CachesLockMmap:   config.CachesLockMmap,
			DatasetDir:       config.DatasetDir,
			DatasetsInMem:    config.DatasetsInMem,
			DatasetsOnDisk:   config.DatasetsOnDisk,
			DatasetsLockMmap: config.DatasetsLockMmap,
		}, notify, noverify)
		engine.SetThreads(-1) // Disable CPU mining
		return engine
	}
}

// APIs return the collection of RPC services the BHEereum package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *BHEereum) APIs() []rpc.API {
	apis := BHEapi.GetAPIs(s.APIBackend)

	// Append any APIs exposed explicitly by the les server
	if s.lesServer != nil {
		apis = append(apis, s.lesServer.APIs()...)
	}
	// Append any APIs exposed explicitly by the consensus engine
	apis = append(apis, s.engine.APIs(s.BlockChain())...)

	// Append any APIs exposed explicitly by the les server
	if s.lesServer != nil {
		apis = append(apis, s.lesServer.APIs()...)
	}

	// Append all the local APIs and return
	return append(apis, []rpc.API{
		{
			Namespace: "BHE",
			Version:   "1.0",
			Service:   NewPublicBHEereumAPI(s),
			Public:    true,
		}, {
			Namespace: "BHE",
			Version:   "1.0",
			Service:   NewPublicMinerAPI(s),
			Public:    true,
		}, {
			Namespace: "BHE",
			Version:   "1.0",
			Service:   downloader.NewPublicDownloaderAPI(s.protocolManager.downloader, s.eventMux),
			Public:    true,
		}, {
			Namespace: "miner",
			Version:   "1.0",
			Service:   NewPrivateMinerAPI(s),
			Public:    false,
		}, {
			Namespace: "BHE",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(s.APIBackend, false),
			Public:    true,
		}, {
			Namespace: "admin",
			Version:   "1.0",
			Service:   NewPrivateAdminAPI(s),
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPublicDebugAPI(s),
			Public:    true,
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPrivateDebugAPI(s),
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   s.netRPCService,
			Public:    true,
		},
	}...)
}

func (s *BHEereum) ResetWithGenesisBlock(gb *types.Block) {
	s.blockchain.ResetWithGenesisBlock(gb)
}

func (s *BHEereum) BHEerbase() (eb common.Address, err error) {
	s.lock.RLock()
	BHEerbase := s.BHEerbase
	s.lock.RUnlock()

	if BHEerbase != (common.Address{}) {
		return BHEerbase, nil
	}
	if wallets := s.AccountManager().Wallets(); len(wallets) > 0 {
		if accounts := wallets[0].Accounts(); len(accounts) > 0 {
			BHEerbase := accounts[0].Address

			s.lock.Lock()
			s.BHEerbase = BHEerbase
			s.lock.Unlock()

			log.Info("BHEerbase automatically configured", "address", BHEerbase)
			return BHEerbase, nil
		}
	}
	return common.Address{}, fmt.Errorf("BHEerbase must be explicitly specified")
}

// isLocalBlock checks whBHEer the specified block is mined
// by local miner accounts.
//
// We regard two types of accounts as local miner account: BHEerbase
// and accounts specified via `txpool.locals` flag.
func (s *BHEereum) isLocalBlock(block *types.Block) bool {
	author, err := s.engine.Author(block.Header())
	if err != nil {
		log.Warn("Failed to retrieve block author", "number", block.NumberU64(), "hash", block.Hash(), "err", err)
		return false
	}
	// Check whBHEer the given address is BHEerbase.
	s.lock.RLock()
	BHEerbase := s.BHEerbase
	s.lock.RUnlock()
	if author == BHEerbase {
		return true
	}
	// Check whBHEer the given address is specified by `txpool.local`
	// CLI flag.
	for _, account := range s.config.TxPool.Locals {
		if account == author {
			return true
		}
	}
	return false
}

// shouldPreserve checks whBHEer we should preserve the given block
// during the chain reorg depending on whBHEer the author of block
// is a local account.
func (s *BHEereum) shouldPreserve(block *types.Block) bool {
	// The reason we need to disable the self-reorg preserving for clique
	// is it can be probable to introduce a deadlock.
	//
	// e.g. If there are 7 available signers
	//
	// r1   A
	// r2     B
	// r3       C
	// r4         D
	// r5   A      [X] F G
	// r6    [X]
	//
	// In the round5, the inturn signer E is offline, so the worst case
	// is A, F and G sign the block of round5 and reject the block of opponents
	// and in the round6, the last available signer B is offline, the whole
	// network is stuck.
	if _, ok := s.engine.(*clique.Clique); ok {
		return false
	}
	return s.isLocalBlock(block)
}

// SetBHEerbase sets the mining reward address.
func (s *BHEereum) SetBHEerbase(BHEerbase common.Address) {
	s.lock.Lock()
	s.BHEerbase = BHEerbase
	s.lock.Unlock()

	s.miner.SetBHEerbase(BHEerbase)
}

// StartMining starts the miner with the given number of CPU threads. If mining
// is already running, this mBHEod adjust the number of threads allowed to use
// and updates the minimum price required by the transaction pool.
func (s *BHEereum) StartMining(threads int) error {
	// Update the thread count within the consensus engine
	type threaded interface {
		SetThreads(threads int)
	}
	if th, ok := s.engine.(threaded); ok {
		log.Info("Updated mining threads", "threads", threads)
		if threads == 0 {
			threads = -1 // Disable the miner from within
		}
		th.SetThreads(threads)
	}
	// If the miner was not running, initialize it
	if !s.IsMining() {
		// Propagate the initial price point to the transaction pool
		s.lock.RLock()
		price := s.gasPrice
		s.lock.RUnlock()
		s.txPool.SetGasPrice(price)

		// Configure the local mining address
		eb, err := s.BHEerbase()
		if err != nil {
			log.Error("Cannot start mining without BHEerbase", "err", err)
			return fmt.Errorf("BHEerbase missing: %v", err)
		}
		if clique, ok := s.engine.(*clique.Clique); ok {
			wallet, err := s.accountManager.Find(accounts.Account{Address: eb})
			if wallet == nil || err != nil {
				log.Error("BHEerbase account unavailable locally", "err", err)
				return fmt.Errorf("signer missing: %v", err)
			}
			clique.Authorize(eb, wallet.SignData)
		}
		// If mining is started, we can disable the transaction rejection mechanism
		// introduced to speed sync times.
		atomic.StoreUint32(&s.protocolManager.acceptTxs, 1)

		go s.miner.Start(eb)
	}
	return nil
}

// StopMining terminates the miner, both at the consensus engine level as well as
// at the block creation level.
func (s *BHEereum) StopMining() {
	// Update the thread count within the consensus engine
	type threaded interface {
		SetThreads(threads int)
	}
	if th, ok := s.engine.(threaded); ok {
		th.SetThreads(-1)
	}
	// Stop the block creating itself
	s.miner.Stop()
}

func (s *BHEereum) IsMining() bool      { return s.miner.Mining() }
func (s *BHEereum) Miner() *miner.Miner { return s.miner }

func (s *BHEereum) AccountManager() *accounts.Manager  { return s.accountManager }
func (s *BHEereum) BlockChain() *core.BlockChain       { return s.blockchain }
func (s *BHEereum) TxPool() *core.TxPool               { return s.txPool }
func (s *BHEereum) EventMux() *event.TypeMux           { return s.eventMux }
func (s *BHEereum) Engine() consensus.Engine           { return s.engine }
func (s *BHEereum) ChainDb() BHEdb.Database            { return s.chainDb }
func (s *BHEereum) IsListening() bool                  { return true } // Always listening
func (s *BHEereum) BHEVersion() int                    { return int(ProtocolVersions[0]) }
func (s *BHEereum) NetVersion() uint64                 { return s.networkID }
func (s *BHEereum) Downloader() *downloader.Downloader { return s.protocolManager.downloader }
func (s *BHEereum) Synced() bool                       { return atomic.LoadUint32(&s.protocolManager.acceptTxs) == 1 }
func (s *BHEereum) ArchiveMode() bool                  { return s.config.NoPruning }

// Protocols implements node.Service, returning all the currently configured
// network protocols to start.
func (s *BHEereum) Protocols() []p2p.Protocol {
	protos := make([]p2p.Protocol, len(ProtocolVersions))
	for i, vsn := range ProtocolVersions {
		protos[i] = s.protocolManager.makeProtocol(vsn)
		protos[i].Attributes = []enr.Entry{s.currentBHEEntry()}
		protos[i].DialCandidates = s.dialCandidates
	}
	if s.lesServer != nil {
		protos = append(protos, s.lesServer.Protocols()...)
	}
	return protos
}

// Start implements node.Service, starting all internal goroutines needed by the
// BHEereum protocol implementation.
func (s *BHEereum) Start(srvr *p2p.Server) error {
	s.startBHEEntryUpdate(srvr.LocalNode())

	// Start the bloom bits servicing goroutines
	s.startBloomHandlers(params.BloomBitsBlocks)

	// Start the RPC service
	s.netRPCService = BHEapi.NewPublicNetAPI(srvr, s.NetVersion())

	// Figure out a max peers count based on the server limits
	maxPeers := srvr.MaxPeers
	if s.config.LightServ > 0 {
		if s.config.LightPeers >= srvr.MaxPeers {
			return fmt.Errorf("invalid peer config: light peer count (%d) >= total peer count (%d)", s.config.LightPeers, srvr.MaxPeers)
		}
		maxPeers -= s.config.LightPeers
	}
	// Start the networking layer and the light server if requested
	s.protocolManager.Start(maxPeers)
	if s.lesServer != nil {
		s.lesServer.Start(srvr)
	}
	return nil
}

// Stop implements node.Service, terminating all internal goroutines used by the
// BHEereum protocol.
func (s *BHEereum) Stop() error {
	// Stop all the peer-related stuff first.
	s.protocolManager.Stop()
	if s.lesServer != nil {
		s.lesServer.Stop()
	}

	// Then stop everything else.
	s.bloomIndexer.Close()
	close(s.closeBloomHandler)
	s.txPool.Stop()
	s.miner.Stop()
	s.blockchain.Stop()
	s.engine.Close()
	s.chainDb.Close()
	s.eventMux.Stop()
	return nil
}
