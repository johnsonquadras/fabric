/*
Licensed to the Apache Software Foundation (ASF) under one
or more contributor license agreements.  See the NOTICE file
distributed with this work for additional information
regarding copyright ownership.  The ASF licenses this file
to you under the Apache License, Version 2.0 (the
"License"); you may not use this file except in compliance
with the License.  You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied.  See the License for the
specific language governing permissions and limitations
under the License.
*/

package ledger

import (
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/op/go-logging"
	"github.com/openblockchain/obc-peer/openchain/db"
	"github.com/openblockchain/obc-peer/protos"
	"github.com/tecbot/gorocksdb"
)

var indexLogger = logging.MustGetLogger("indexes")
var prefixBlockHashKey = byte(1)
var prefixTxGUIDKey = byte(2)
var prefixAddressBlockNumCompositeKey = byte(3)

type blockchainIndexer interface {
	isSynchronous() bool
	start() error
	createIndexesSync(block *protos.Block, blockNumber uint64, blockHash []byte, writeBatch *gorocksdb.WriteBatch) error
	createIndexesAsync(block *protos.Block, blockNumber uint64, blockHash []byte) error
	fetchBlockNumberByBlockHash(blockHash []byte) (uint64, error)
	fetchTransactionIndexByGUID(txGUID []byte) (uint64, uint64, error)
	stop()
}

// Implementation for sync indexer
type blockchainIndexerSync struct {
}

func newBlockchainIndexerSync() *blockchainIndexerSync {
	return &blockchainIndexerSync{}
}

func (indexer *blockchainIndexerSync) isSynchronous() bool {
	return true
}

func (indexer *blockchainIndexerSync) start() error {
	return nil
}

func (indexer *blockchainIndexerSync) createIndexesSync(
	block *protos.Block, blockNumber uint64, blockHash []byte, writeBatch *gorocksdb.WriteBatch) error {
	return addIndexDataForPersistence(block, blockNumber, blockHash, writeBatch)
}

func (indexer *blockchainIndexerSync) createIndexesAsync(block *protos.Block, blockNumber uint64, blockHash []byte) error {
	return fmt.Errorf("Method not applicable")
}

func (indexer *blockchainIndexerSync) fetchBlockNumberByBlockHash(blockHash []byte) (uint64, error) {
	return fetchBlockNumberByBlockHashFromDB(blockHash)
}

func (indexer *blockchainIndexerSync) fetchTransactionIndexByGUID(txGUID []byte) (uint64, uint64, error) {
	return fetchTransactionIndexByGUIDFromDB(txGUID)
}

func (indexer *blockchainIndexerSync) stop() {
	return
}

// Functions for persisting and retrieving index data
func addIndexDataForPersistence(block *protos.Block, blockNumber uint64, blockHash []byte, writeBatch *gorocksdb.WriteBatch) error {
	openchainDB := db.GetDBHandle()
	cf := openchainDB.IndexesCF

	// add blockhash -> blockNumber
	indexLogger.Debug("Indexing block number [%d] by hash = [%x]", blockNumber, blockHash)
	writeBatch.PutCF(cf, encodeBlockHashKey(blockHash), encodeBlockNumber(blockNumber))

	addressToTxIndexesMap := make(map[string][]uint64)
	addressToChainletIDsMap := make(map[string][]*protos.ChainletID)

	transactions := block.GetTransactions()
	for txIndex, tx := range transactions {
		// add TxGUID -> (blockNumber,indexWithinBlock)
		writeBatch.PutCF(cf, encodeTxGUIDKey(getTxGUIDBytes(tx)), encodeBlockNumTxIndex(blockNumber, uint64(txIndex)))

		txExecutingAddress := getTxExecutingAddress(tx)
		addressToTxIndexesMap[txExecutingAddress] = append(addressToTxIndexesMap[txExecutingAddress], uint64(txIndex))

		switch tx.Type {
		case protos.Transaction_CHAINLET_NEW, protos.Transaction_CHAINLET_UPDATE:
			authroizedAddresses, chainletID := getAuthorisedAddresses(tx)
			for _, authroizedAddress := range authroizedAddresses {
				addressToChainletIDsMap[authroizedAddress] = append(addressToChainletIDsMap[authroizedAddress], chainletID)
			}
		}
	}
	for address, txsIndexes := range addressToTxIndexesMap {
		writeBatch.PutCF(cf, encodeAddressBlockNumCompositeKey(address, blockNumber), encodeListTxIndexes(txsIndexes))
	}
	return nil
}

func fetchBlockNumberByBlockHashFromDB(blockHash []byte) (uint64, error) {
	blockNumberBytes, err := db.GetDBHandle().GetFromIndexesCF(encodeBlockHashKey(blockHash))
	if err != nil {
		return 0, err
	}
	blockNumber := decodeBlockNumber(blockNumberBytes)
	return blockNumber, nil
}

func fetchTransactionIndexByGUIDFromDB(txGUID []byte) (uint64, uint64, error) {
	blockNumTxIndexBytes, err := db.GetDBHandle().GetFromIndexesCF(encodeTxGUIDKey(txGUID))
	if err != nil {
		return 0, 0, err
	}
	return decodeBlockNumTxIndex(blockNumTxIndexBytes)
}

// functions for retrieving data from proto-structures
func getTxGUIDBytes(tx *protos.Transaction) (guid []byte) {
	guid = []byte("TODO:Fetch guid from Tx")
	return
}

func getTxExecutingAddress(tx *protos.Transaction) string {
	// TODO Fetch address form tx
	return "address1"
}

func getAuthorisedAddresses(tx *protos.Transaction) ([]string, *protos.ChainletID) {
	// TODO fetch address from chaincode deployment tx
	return []string{"address1", "address2"}, tx.GetChainletID()
}

// functions for encoding/decoding db keys/values for index data
// encode / decode BlockNumber
func encodeBlockNumber(blockNumber uint64) []byte {
	return proto.EncodeVarint(blockNumber)
}

func decodeBlockNumber(blockNumberBytes []byte) (blockNumber uint64) {
	blockNumber, _ = proto.DecodeVarint(blockNumberBytes)
	return
}

// encode / decode BlockNumTxIndex
func encodeBlockNumTxIndex(blockNumber uint64, txIndexInBlock uint64) []byte {
	b := proto.NewBuffer([]byte{})
	b.EncodeVarint(blockNumber)
	b.EncodeVarint(txIndexInBlock)
	return b.Bytes()
}

func decodeBlockNumTxIndex(bytes []byte) (blockNum uint64, txIndex uint64, err error) {
	b := proto.NewBuffer(bytes)
	blockNum, err = b.DecodeVarint()
	if err != nil {
		return
	}
	txIndex, err = b.DecodeVarint()
	if err != nil {
		return
	}
	return
}

// encode BlockHashKey
func encodeBlockHashKey(blockHash []byte) []byte {
	return prependKeyPrefix(prefixBlockHashKey, blockHash)
}

// encode TxGUIDKey
func encodeTxGUIDKey(guidBytes []byte) []byte {
	return prependKeyPrefix(prefixTxGUIDKey, guidBytes)
}

func encodeAddressBlockNumCompositeKey(address string, blockNumber uint64) []byte {
	b := proto.NewBuffer([]byte{prefixAddressBlockNumCompositeKey})
	b.EncodeRawBytes([]byte(address))
	b.EncodeVarint(blockNumber)
	return b.Bytes()
}

func encodeListTxIndexes(listTx []uint64) []byte {
	b := proto.NewBuffer([]byte{})
	for i := range listTx {
		b.EncodeVarint(listTx[i])
	}
	return b.Bytes()
}

func encodeChainletID(c *protos.ChainletID) []byte {
	// TODO serialize chainletID
	return []byte{}
}

func prependKeyPrefix(prefix byte, key []byte) []byte {
	modifiedKey := []byte{}
	modifiedKey = append(modifiedKey, prefix)
	modifiedKey = append(modifiedKey, key...)
	return modifiedKey
}