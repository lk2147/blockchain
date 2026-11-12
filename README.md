# Blockchain Lab01

Mỗi node là một process Go riêng, giao tiếp qua mạng localhost thực:

```text
Client ký và gửi transaction qua gRPC Submit
    -> node tiếp nhận kiểm tra và lưu mempool trong RAM
    -> GossipSub truyền transaction đến các peer
    -> mỗi peer tự kiểm tra và lưu mempool riêng
    -> proposer chọn transaction liên tiếp theo nonce, tạo block
    -> gossip header -> chấp nhận header -> yêu cầu body qua gRPC bi-stream
    -> kiểm tra body, thực thi, PREVOTE, PRECOMMIT
    -> ghi block + chứng nhận vào LevelDB, Sync=true
    -> cập nhật state, loại transaction đã final khỏi mempool
```

Không còn transaction tạo sẵn trong config. Node không tự tạo giao dịch hay
final block rỗng khi chưa có transaction. Không có ledger chung hoặc controller
quyết định thay các validator. Mặc định 10 validator, f=3, quorum=7; số node
phải thỏa n>=8 và n=3f+1.

## Chuẩn bị

Cần Python 3.9+ và Go đúng phiên bản trong go.mod. Launcher dùng Python standard
library. Go modules tự tải libp2p, gRPC và GoLevelDB; không cần cài database server
hoặc thư viện C++. Mở terminal tại thư mục chứa README.md và go.mod.

Tạo key và config mới, chưa chạy node:

```text
python tests/run_local_cluster.py --run-dir local-runs/demo --run-ms 0 --prepare-only
```

Nếu Go chưa nằm trong PATH, thêm --go D:/tools/golang/bin/go.exe.
Thư mục mới chứa network.json, key-00.hex đến key-09.hex và node-00.json đến
node-09.json. Key được sinh bằng crypto/rand; không dùng seed của fault layer.
Mặc định cần các cổng gRPC 29000-29009 và libp2p 29010-29019.

## Một node trên mỗi terminal

Mở 10 terminal tại thư mục project, lần lượt chạy --node 0 đến --node 9:

```text
python tests/run_local_cluster.py --config-dir local-runs/demo --node 0
```

```text
python tests/run_local_cluster.py --config-dir local-runs/demo --node 1
```

Các terminal còn lại dùng cùng lệnh với ID tương ứng. Mỗi wrapper chạy một
process Go bằng file config của node đó, không rebuild binary đang được dùng.
RunForMS=0 nghĩa là chạy đến khi Ctrl+C. Node chờ transaction trong mempool.

Để khởi động toàn bộ bằng một lệnh, dùng --config-dir mà không có --node:

```text
python tests/run_local_cluster.py --config-dir local-runs/demo
```

Chọn một cách chạy cho mỗi thí nghiệm. Khi launcher chạy cả mạng, gửi transaction
trong terminal khác. Ctrl+C khi chạy từng node cho phép đóng DB và lưu kết quả.
Khi dùng launcher cả mạng, Ctrl+C dừng các child; block đã Sync xuống disk vẫn
còn, nhưng result.json có thể chưa được tạo.

## Config gọn cho từng node

Ví dụ node-00.json:

```json
{
  "NodeID": 0,
  "NetworkFile": "network.json",
  "Behavior": "honest",
  "SendGapMS": 10
}
```

network.json chứa ChainID, F và danh sách Peers dùng chung. Mỗi peer có Address,
GossipAddress, PublicKey. Thứ tự public key xác định lịch proposer. Launcher kiểm
tra các node dùng cùng danh sách; stream handshake kiểm tra fingerprint đã ký.
Không có cập nhật validator membership trong lúc chạy.

Các trường có thể thêm khi muốn đổi mặc định:

| Trường | Mặc định | Ý nghĩa |
|---|---|---|
| SendGapMS | 1 | Khoảng cách tối thiểu giữa các lần phát protocol của node; 10ms tương ứng tối đa khoảng 100 lần/giây |
| Behavior | honest | Hành vi giao thức của node |
| BaseTimeoutMS | 2000 | Timeout cơ sở, tăng theo round |
| RetryMS | 400 | Chu kỳ retry vote/proposal và transaction do client nộp tại node này |
| RunForMS | 0 | 0 chạy đến Ctrl+C; giá trị dương tự dừng sau số ms tương ứng |
| TargetHeights | không giới hạn | Giới hạn chiều cao node tự đề xuất, dùng cho thí nghiệm hữu hạn |
| MempoolLimit | 4096 | Số transaction tối đa đang chờ |
| MempoolBytes | 8388608 | Giới hạn tổng byte JSON transaction đang chờ |
| MaxTxBytes | 65536 | Kích thước JSON tối đa mỗi transaction |
| MaxBlockTXs | 128 | Số transaction tối đa trong proposal |
| MaxBlockBytes | 4177920 | Giới hạn JSON body, chừa chỗ cho envelope dưới giới hạn gRPC 4 MiB |
| Trace | false | true ghi đầy đủ sự kiện network/protocol để kiểm chứng |

PrivateKeyFile và OutputDir mặc định lần lượt là key-NN.hex và node-NN, tính
tương đối với file config. Đổi config trước khi chạy; không reload trực tiếp.
Mọi node đều có outbound limit, kể cả dishonest node.

## Gửi transaction thật qua mạng

Trong terminal khác, dùng key của node 0 để ký, nộp vào node 9:

```text
python tests/run_local_cluster.py --config-dir local-runs/demo --node 0 --via 9 --submit hello --record message --nonce 1
```

--via là node tiếp nhận, mặc định trùng --node. --node chọn key người ký khi
dùng --submit; lệnh này không khởi động thêm validator. Vì vậy key node 0 vẫn
có thể ký transaction gửi qua node 9 khi process validator 0 đã crash.

Lệnh trả transaction ID khi mempool tiếp nhận, chưa phải xác nhận finalization.
Xem state_commit trong log. Lần tiếp theo cùng sender dùng --nonce 2, rồi 3...
Transaction chỉ được sửa key <public-key-hex>/<record> thuộc sender. Không có
phí/gas/token. Transaction sai chữ ký, sai chain, ownership, replay hoặc vượt
giới hạn sẽ bị từ chối. Nonce tương lai được giữ có giới hạn, nhưng chỉ nonce
liên tiếp mới được chọn vào block.

Các mempool có thể khác nhau tạm thời do network. Validator vẫn kiểm tra toàn
bộ transaction của proposal ngay cả khi chưa từng thấy nó trong mempool. Nếu
proposal thất bại, transaction còn chờ; chỉ finalization mới xóa nonce đã dùng.

## Dishonest node

Đổi Behavior trong config trước khi chạy. Số node dishonest không vượt f khi
muốn kiểm tra safety theo giả định Lab01; launcher từ chối cấu hình vượt giới hạn.

| Behavior | Hành động thực tế |
|---|---|
| honest | Chỉ phát proposal/vote theo validation, quorum và locking rules |
| invalid_signature | Làm hỏng chữ ký proposal/vote do chính node tạo |
| wrong_domain | Ký proposal/vote bằng context khác; peer phải từ chối |
| duplicate_transaction | Phát thêm bản sao transaction vừa tiếp nhận |
| replay_transaction | Phát lại transaction cũ sau khi block đã final |
| silent_proposer | Bỏ qua lượt đề xuất của mình; vẫn xử lý lượt của peer |
| crashed_proposer | Process kết thúc với code 86 khi tới lượt đề xuất; peer phải timeout và đổi round |

Dishonest node chỉ có key của chính nó, không thể ký thay honest peer. Header và
vote của node honest vẫn được xác thực, không bỏ guard để thí nghiệm chạy tiếp.
T7 còn có driver equivocation chuyên biệt trong test; không gán ledger chung
hoặc sửa trạng thái honest node từ bên ngoài.

## Delay/drop/duplicate/reorder/replay trên đường truyền

Có thể thêm Faults vào bất kỳ node nào, độc lập với Behavior. Ví dụ giữ mọi
message do honest node 0 phát ít nhất 500ms:

```json
"Faults": {
  "Seed": 1,
  "MinDelayMS": 500,
  "MaxDelayMS": 500
}
```

Các tùy chọn khác: DropBPS, DuplicateBPS (1000=10%, 10000=100%), ReorderEvery,
ReorderHoldMS, ReplayEvery, ReplayAgeMS, DisableAfterMS (0 nghĩa không tự tắt).
ReceiveLimit/ReceiveWindowMS/BlockMS giới hạn và tạm block sender quá tải.

Fault layer và SendGapMS áp dụng trước gossip publication do node khởi phát và
directed gRPC write. Heartbeat/control và automatic forwarding của libp2p không
bị điều chỉnh bởi lớp này. Không tuyên bố giới hạn toàn bộ TCP packet/bandwidth
hay bảo đảm thời gian cứng của OS. GossipSub có thể truyền qua peer trung gian;
gRPC Exchange cho phép hai đầu gửi body/request trên cùng stream và reconnect.
Client Submit là unary RPC tiếp nhận giao dịch, không thay thế peer bi-stream.

## Ledger trên disk và log

Mỗi node tạo thư mục riêng, ví dụ local-runs/demo/node-09:

- ledger.db/: GoLevelDB chứa toàn bộ block đã final, transaction và certificate.
- events.jsonl: mặc định chỉ sự kiện quan trọng, hash đọc ở dạng hex.
- result.json: metadata kết quả, height và state root; không chứa bản sao ledger.
- ready: node đã mở server; running.lock ngăn hai process dùng cùng output.

WriteBatch cập nhật block và height một cách atomic, Sync=true trước khi công
bố state_commit. Không đợi shutdown mới ghi block. Node không giữ toàn bộ lịch
sử body block trong RAM; RAM giữ mempool, state hiện tại, candidate và index/header
nhẹ để phục vụ catch-up. Body đã final được đọc từ DB khi peer yêu cầu.

Trace=false hiển thị các sự kiện mempool_accept, transaction_reject, proposal,
vote, lock, timeout và state_commit. Rejection/fault lặp được giảm tần suất.
Muốn kiểm tra từng packet, planned hold và actual send thì bật Trace=true; bộ
test dùng chế độ này. Log gọn không dùng để khẳng định mọi bản sao đều đã gửi.

Sau khi dừng node, đọc ledger từ LevelDB:

```text
python tests/run_local_cluster.py --config-dir local-runs/demo --node 9 --inspect
python tests/run_local_cluster.py --config-dir local-runs/demo --check-only
```

--inspect là thao tác offline; LevelDB khóa DB của process đang chạy.
--check-only đọc metadata và gọi Go để đọc DB, so sánh history/root của các node
honest. Go integration tests còn thực thi lại và xác minh từng certificate.

Block đã final vẫn có thể đọc sau khi process bị kill. **Khôi phục an toàn một
validator vào mạng đang chạy chưa được triển khai**: còn cần journal vote/lock
để không ký hai lần sau restart. Node sẽ từ chối dùng lại output cũ thay vì tự
quên signing history. Dùng thư mục mới cho thí nghiệm mới; không xóa ledger để
giả làm recovery. Mempool chỉ nằm trong RAM và mất khi process dừng.

## Kiểm tra

```text
python tests/run_tests.py
python tests/run_tests.py --race
python tests/verify_repeatability.py
```

Thêm --go D:/tools/golang/bin/go.exe khi cần. T1-T8 và sáu behavior cấu hình chạy
trên các process/sockets thật. Transaction hợp lệ đi vào qua client RPC và
GossipSub; không đọc workload cố định từ config. Verifier đọc block từ LevelDB,
kiểm tra parent, execution, nonce, root và ít nhất 7 chữ ký PRECOMMIT khác nhau.
Unit tests kiểm tra mempool, durable write, đọc lại sau kill process và không
commit nếu ghi disk thất bại. Xem tests/TEST_STATUS.md cho kết quả đã thực thi.

## Code và byte được ký

| Package | Trách nhiệm |
|---|---|
| src/crypto, src/types | Kiểu dữ liệu, Ed25519, SHA-256, canonical bytes |
| src/state, src/block | Ownership/nonce, thực thi atomic, Merkle root và validation |
| src/mempool | Pending transaction trong RAM có giới hạn và selection theo nonce |
| src/ledger | LevelDB, block theo height, durable atomic append |
| src/consensus | PREVOTE/PRECOMMIT, lock, timeout, certificate |
| src/node | Nối mempool, consensus, disk và message flow |
| src/realnet | GossipSub, gRPC, client ingress, fault controls, config và log |

Canonical payload bắt đầu bằng version byte 1 và context UTF-8 có uint32 length.
Integer dùng big-endian; string/bytes dùng uint32 byte length; hash/key/signature
có độ dài cố định 32/32/64 byte. Signature không nằm trong payload của chính nó.

| Domain | Signed fields sau context |
|---|---|
| TX:<chain> | sender, target key, value, nonce |
| HEADER:<chain> | height, parent hash, body hash, state root, proposer |
| VOTE:<chain> | height, round, phase, validator, NIL marker, block hash |

TransactionID bao gồm canonical TX và signature. BodyHash cam kết thứ tự signed
transaction; StateRoot cam kết state và nonce bằng deterministic Merkle tree.
PROPOSAL và TRANSPORT có domain riêng; không ký protobuf wire bytes tùy ý.
Các comment trong src/crypto/encoding.go, src/consensus/messages.go và
src/realnet/auth.go mô tả đầy đủ layout. Schema gRPC ở src/proto/consensus.proto.

Tham khảo: [GoLevelDB](https://github.com/syndtr/goleveldb),
[GossipSub](https://github.com/libp2p/go-libp2p-pubsub),
[gRPC Go](https://grpc.io/docs/languages/go/basics/). Project vẫn là lab localhost,
không triển khai public network hoặc một framework blockchain tổng quát.
