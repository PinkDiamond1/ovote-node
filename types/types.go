package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-iden3-crypto/poseidon"
	"github.com/vocdoni/arbo"
)

// ProcessStatus type is used to define the status of the Process
type ProcessStatus int

var (
	hashLen int = arbo.HashFunctionPoseidon.Len()

	// ProcessStatusOn indicates that the process is accepting vote (Voting
	// phase)
	ProcessStatusOn ProcessStatus = 0
	// ProcessStatusFrozen indicates that the process is in
	// ResultsPublishingPhase and is no longer accepting new votes, the
	// proof can now be generated
	ProcessStatusFrozen ProcessStatus = 1
	// ProcessStatusProofGenerating indicates that the process is no longer
	// accepting new votes and the zkProof is being generated
	ProcessStatusProofGenerating ProcessStatus = 2
	// ProcessStatusProofGenerated indicates that the process is finished,
	// and the zkProof is already generated
	ProcessStatusProofGenerated ProcessStatus = 3
)

// ByteArray is a type alias over []byte to implement custom json marshalers in
// hex
type ByteArray []byte

// MarshalJSON implements the Marshaler interface for ByteArray types
func (b ByteArray) MarshalJSON() ([]byte, error) {
	return json.Marshal(hex.EncodeToString(b))
}

// UnmarshalJSON implements the Unmarshaler interface for ByteArray types
func (b *ByteArray) UnmarshalJSON(j []byte) error {
	var s string
	err := json.Unmarshal(j, &s)
	if err != nil {
		return err
	}
	b2, err := hex.DecodeString(s)
	if err != nil {
		return err
	}
	*b = ByteArray(b2)
	return nil
}

// CensusProof contains the proof of a PublicKey in the Census Tree
type CensusProof struct {
	Index       uint64             `json:"index"`
	PublicKey   *babyjub.PublicKey `json:"publicKey"`
	MerkleProof ByteArray          `json:"merkleProof"`
}

// VotePackage represents the vote sent by the User
type VotePackage struct {
	Signature   babyjub.SignatureComp `json:"signature"`
	CensusProof CensusProof           `json:"censusProof"`
	Vote        ByteArray             `json:"vote"`
}

// Process represents a voting process
type Process struct {
	// ID is determined by the SmartContract, is unique for each Process
	ID uint64
	// CensusRoot indicates the CensusRoot of the Process, the same
	// CensusRoot can be reused by different Processes
	CensusRoot []byte
	// CensusSize determines the number of public keys placed in the
	// CensusTree leaves under the CensusRoot
	CensusSize uint64
	// EthBlockNum indicates at which Ethereum block number the process has
	// been created
	EthBlockNum uint64
	// ResPubStartBlock (Results Publishing Start Block) indicates the
	// EthBlockNum where the process ends, in which the results can be
	// published
	ResPubStartBlock uint64
	// ResPubWindow (Results Publishing Window) indicates the window of
	// time (blocks), which starts at the ResPubStartBlock and ends at
	// ResPubStartBlock+ResPubWindow.  During this window of time, the
	// results + zkProofs can be sent to the SmartContract.
	ResPubWindow uint64
	// MinParticipation sets a threshold of minimum number of votes over
	// the total users in the census (% over CensusSize)
	MinParticipation uint8
	// MinPositiveVotes sets a threshold of minimum votes supporting the
	// proposal, over all the processed votes (% over nVotes)
	MinPositiveVotes uint8
	// InsertedDatetime contains the datetime of when the process was
	// inserted in the db
	InsertedDatetime time.Time
	// Status determines the current status of the process
	Status ProcessStatus
}

// HashVote computes the vote hash following the circuit approach
func HashVote(chainID, processID uint64, vote []byte) (*big.Int, error) {
	voteBI := arbo.BytesToBigInt(vote)
	signedMsg, err := poseidon.Hash([]*big.Int{
		big.NewInt(int64(chainID)),
		big.NewInt(int64(processID)),
		voteBI,
	})
	if err != nil {
		return nil, err
	}
	return signedMsg, nil
}

func (vp *VotePackage) verifySignature(chainID, processID uint64) error {
	msgToSign, err := HashVote(chainID, processID, vp.Vote)
	if err != nil {
		return err
	}

	sigUncompressed, err := vp.Signature.Decompress()
	if err != nil {
		return err
	}
	v := vp.CensusProof.PublicKey.VerifyPoseidon(
		msgToSign, sigUncompressed)
	if !v {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}
func (vp *VotePackage) verifyMerkleProof(root []byte) error {
	indexBytes := Uint64ToIndex(vp.CensusProof.Index)
	pubKHashBytes, err := HashPubKBytes(vp.CensusProof.PublicKey)
	if err != nil {
		return err
	}
	v, err := arbo.CheckProof(arbo.HashFunctionPoseidon, indexBytes,
		pubKHashBytes, root, vp.CensusProof.MerkleProof)
	if err != nil {
		return err
	}
	if !v {
		return fmt.Errorf("merkleproof verification failed")
	}
	return nil
}

// Verify checks the signature and merkleproof of the VotePackage
func (vp *VotePackage) Verify(chainID, processID uint64, root []byte) error {
	if err := vp.verifySignature(chainID, processID); err != nil {
		return err
	}
	if err := vp.verifyMerkleProof(root); err != nil {
		return err
	}
	// TODO ensure that vote value is 0 or 1
	return nil
}

// Uint64ToIndex returns the bytes representation of the given uint64 that will
// be used as a leaf index in the MerkleTree
func Uint64ToIndex(u uint64) []byte {
	return arbo.BigIntToBytes(MaxKeyLen, big.NewInt(int64(int(u)))) //nolint:gomnd
}

// HashPubKBytes returns the bytes representation of the Poseidon hash of the
// given PublicKey, that will be used as a leaf value in the MerkleTree
func HashPubKBytes(pubK *babyjub.PublicKey) ([]byte, error) {
	pubKHash, err := poseidon.Hash([]*big.Int{pubK.X, pubK.Y})
	if err != nil {
		return nil, err
	}
	return arbo.BigIntToBytes(hashLen, pubKHash), nil
}

//
// // SignatureCompressedSize sets the size of the compressed Signature byte array
// const SignatureCompressedSize = 64
//
// // Signature contains a babyjubjub compressed Signature
// type Signature [SignatureCompressedSize]byte

// HexToPublicKey converts the given hex representation of a
// babyjub.PublicKeyComp, and returns the babyjub.PublicKey
func HexToPublicKey(h string) (*babyjub.PublicKey, error) {
	// pubKStr := c.Param("pubkey")
	// var pubK babyjub.PublicKey
	// err = json.Unmarshal([]byte(pubKStr), &pubK)
	// if err != nil {
	//         returnErr(c, err)
	//         return
	// }

	pubKCompBytes, err := hex.DecodeString(h)
	if err != nil {
		return nil, err
	}
	var pubKComp babyjub.PublicKeyComp
	if len(pubKComp[:]) != len(pubKCompBytes) {
		return nil, fmt.Errorf("unexpected pubK length: %d", len(pubKCompBytes))
	}
	copy(pubKComp[:], pubKCompBytes)
	pubK, err := pubKComp.Decompress()
	if err != nil {
		return nil, err
	}

	return pubK, nil
}
