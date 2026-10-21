package parser

// Bytecode of LuaJIT (lj_bc.h). The parser emits the instructions LuaJIT
// emits for a chunk, see codegen.go, but keeps them only while a function
// is being parsed.

// bcOp is a bytecode opcode. The order is the order of BCDEF: the code
// generator computes opcodes from each other, like a comparison from its
// inverse.
type bcOp uint32

const (
	bcISLT bcOp = iota
	bcISGE
	bcISLE
	bcISGT
	bcISEQV
	bcISNEV
	bcISEQS
	bcISNES
	bcISEQN
	bcISNEN
	bcISEQP
	bcISNEP
	bcISTC
	bcISFC
	bcIST
	bcISF
	bcISTYPE
	bcISNUM
	bcMOV
	bcNOT
	bcUNM
	bcLEN
	bcADDVN
	bcSUBVN
	bcMULVN
	bcDIVVN
	bcMODVN
	bcADDNV
	bcSUBNV
	bcMULNV
	bcDIVNV
	bcMODNV
	bcADDVV
	bcSUBVV
	bcMULVV
	bcDIVVV
	bcMODVV
	bcPOW
	bcCAT
	bcKSTR
	bcKCDATA
	bcKSHORT
	bcKNUM
	bcKPRI
	bcKNIL
	bcUGET
	bcUSETV
	bcUSETS
	bcUSETN
	bcUSETP
	bcUCLO
	bcFNEW
	bcTNEW
	bcTDUP
	bcGGET
	bcGSET
	bcTGETV
	bcTGETS
	bcTGETB
	bcTGETR
	bcTSETV
	bcTSETS
	bcTSETB
	bcTSETM
	bcTSETR
	bcCALLM
	bcCALL
	bcCALLMT
	bcCALLT
	bcITERC
	bcITERN
	bcVARG
	bcISNEXT
	bcRETM
	bcRET
	bcRET0
	bcRET1
	bcFORI
	bcJFORI
	bcFORL
	bcIFORL
	bcJFORL
	bcITERL
	bcIITERL
	bcJITERL
	bcLOOP
	bcILOOP
	bcJLOOP
	bcJMP
	bcFUNCF
	bcIFUNCF
	bcJFUNCF
	bcFUNCV
	bcIFUNCV
	bcJFUNCV
	bcFUNCC
	bcFUNCCW
)

// Instruction fields and limits.
const (
	bcMaxA  = 0xff
	bcMaxB  = 0xff //nolint:goconst // A, B and C are bytes.
	bcMaxC  = 0xff //nolint:goconst // A, B and C are bytes.
	bcMaxD  = 0xffff
	bcBiasJ = 0x8000
	noReg   = bcMaxA //nolint:goconst // An A operand without a register.
	noJump  = ^uint32(0)
)

// bcIns is an instruction: the opcode in bits 0-7, A in bits 8-15, and
// either C in bits 16-23 and B in bits 24-31 or D in bits 16-31.
type bcIns uint32

func (i bcIns) op() bcOp       { return bcOp(i & 0xff) }
func (i bcIns) a() uint32      { return uint32(i>>8) & 0xff }
func (i bcIns) c() uint32      { return uint32(i>>16) & 0xff }
func (i bcIns) d() uint32      { return uint32(i >> 16) }
func (i bcIns) j() int64       { return int64(i.d()) - bcBiasJ }
func (i *bcIns) setOp(x bcOp)  { *i = *i&^0xff | bcIns(x&0xff) }
func (i *bcIns) setA(x uint32) { *i = *i&^0xff00 | bcIns(x&0xff)<<8 }
func (i *bcIns) setB(x uint32) { *i = *i&^0xff000000 | bcIns(x&0xff)<<24 }
func (i *bcIns) setD(x uint32) { *i = *i&^0xffff0000 | bcIns(x&0xffff)<<16 }
func (i *bcIns) setJ(x int64)  { i.setD(uint32(x + bcBiasJ)) }

// insABC, insAD and insAJ compose instructions like BCINS_ABC, BCINS_AD
// and BCINS_AJ, with the same overflow of out-of-range operands.
func insABC(o bcOp, a, b, c uint32) bcIns {
	return bcIns(uint32(o) | a<<8 | b<<24 | c<<16)
}

func insAD(o bcOp, a, d uint32) bcIns { return bcIns(uint32(o) | a<<8 | d<<16) }

func insAJ(o bcOp, a uint32, j int64) bcIns { return insAD(o, a, uint32(j+bcBiasJ)) }

// isRetOrTail reports whether an opcode returns from the function
// (bc_isret_or_tail).
func isRetOrTail(op bcOp) bool {
	switch op {
	case bcCALLMT, bcCALLT, bcRETM, bcRET, bcRET0, bcRET1:
		return true
	default:
		return false
	}
}
