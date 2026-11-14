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
Mặc định cần các cổng gRPC 29000-29009, libp2p 29010-29019 và HTTP admin
29020-29029.

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
  "AdminAddress": "127.0.0.1:29020",
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
| AdminAddress | 127.0.0.1:0 | HTTP read-only để xem status, mempool và ledger; `:0` chọn port localhost trống |

PrivateKeyFile và OutputDir mặc định lần lượt là key-NN.hex và node-NN, tính
tương đối với file config. Đổi config trước khi chạy; không reload trực tiếp.
Mọi node đều có outbound limit, kể cả dishonest node.

## Tạo một test mới bằng cách copy config

Cách đơn giản nhất là giữ một bộ config sạch làm mẫu, sau đó copy các file đó
sang một thư mục test mới và chỉ sửa những giá trị muốn thử. Không cần viết lại
validator list, địa chỉ mạng hoặc key.

### 1. Tạo bộ config mẫu một lần

```text
python tests/run_local_cluster.py --run-dir local-runs/template-base --run-ms 0 --prepare-only
```

Không chạy node bằng thư mục `template-base`; giữ nó làm mẫu. Nếu thư mục này đã
có sẵn thì bỏ qua bước trên.

### 2. Copy sang test mới

Trong File Explorer, tạo một thư mục mới, ví dụ `local-runs/test-delay`, rồi copy
ba nhóm file sau từ `local-runs/template-base` vào đó:

```text
network.json
node-00.json ... node-09.json
key-00.hex ... key-09.hex
```

Không copy các thư mục `node-00/` ... `node-09/`, file `stdout-*`, `stderr-*`
hoặc `node.exe`. Chúng là database/log/kết quả của lần chạy cũ. Mỗi test phải có
thư mục mới để LevelDB và log không bị trộn hoặc ghi đè.

Các đường dẫn `NetworkFile`, `PrivateKeyFile` và `OutputDir` là đường dẫn tương
đối, nên vẫn hoạt động sau khi copy mà không cần sửa. Có thể giữ nguyên port nếu
chỉ chạy một cluster tại một thời điểm. Nếu chạy hai cluster đồng thời thì phải
đổi toàn bộ `Address` và `GossipAddress` trong `network.json` của một cluster.

### 3. Chỉ sửa value cần kiểm tra

Ví dụ test node 0 là honest node nhưng mọi message do nó khởi phát bị giữ 500ms,
chỉ cần sửa `node-00.json` thành:

```json
{
  "NodeID": 0,
  "NetworkFile": "network.json",
  "Behavior": "honest",
  "SendGapMS": 1,
  "Trace": true,
  "Faults": {
    "Seed": 1,
    "MinDelayMS": 500,
    "MaxDelayMS": 500
  }
}
```

Ví dụ test proposer im lặng, chỉ đổi một value trong `node-00.json`:

```json
"Behavior": "silent_proposer"
```

Không cần thêm `Faults` nếu test chỉ kiểm tra behavior. Có thể kết hợp `Behavior`
và `Faults` trong cùng một node. Khi kiểm tra giả định an toàn của Lab01, tổng số
node có `Behavior` khác `honest` không được vượt quá `f=3`; launcher sẽ kiểm tra
điều này trước khi chạy.

Nếu muốn các process tự kết thúc, thêm cùng một giá trị `RunForMS`, ví dụ `15000`,
vào **tất cả** `node-00.json` ... `node-09.json`. Nếu để `0` hoặc bỏ trường này,
node chạy đến khi nhấn Ctrl+C.

### 4. Chạy test đã copy

Chạy cả cluster:

```text
python tests/run_local_cluster.py --config-dir local-runs/test-delay
```

Hoặc chạy mỗi node trong một terminal bằng `--node 0` ... `--node 9`. Trong một
terminal khác, gửi transaction thật:

```text
python tests/run_local_cluster.py --config-dir local-runs/test-delay --node 0 --via 9 --submit test-delay --record message --nonce 1
```

Sau khi các node đã dừng, kiểm tra ledger và state của các honest node:

```text
python tests/run_local_cluster.py --config-dir local-runs/test-delay --check-only
```

Muốn tạo test tiếp theo, quay lại copy từ `template-base`, không copy từ một test
đã chạy. Nhờ vậy mỗi test luôn bắt đầu với mempool rỗng, ledger rỗng và config
gốc dễ đối chiếu.

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

### Gửi hàng loạt transaction bằng key ngoài validator

Khi cluster đang chạy, lệnh sau tạo một cặp Ed25519 mới bằng `crypto/rand`. Public
key không thuộc validator set; private key được lưu trong thư mục test và chỉ
dùng làm client. Sau đó script gửi liên tục 20.000 transaction có nonce 1..20000:

```text
python tests/send_bulk_transactions.py --config-dir local-runs/test-delay --via 9 --count 20000 --rate 200
```

`--via 9` phải là một honest node đang chạy. `--rate 200` giới hạn tối đa khoảng
200 transaction được nhận mỗi giây; dùng `0` để gửi nhanh nhất có thể. Có thể đổi
kích thước value bằng `--value-size`, tối đa 32768 byte trong script này:

```text
python tests/send_bulk_transactions.py --config-dir local-runs/test-delay --via 9 --count 15000 --rate 100 --value-size 1024
```

Mỗi lần chạy script luôn tạo file `external-client-<timestamp>.hex` mới và từ
chối ghi đè/reuse key cũ. Script kiểm tra public key mới không trùng bất kỳ
validator nào. Transaction vẫn hợp lệ vì ownership dựa trên sender: target key
có dạng `<external-public-key>/bulk`; người dùng không cần là validator để gửi
transaction.

Script chỉ gửi transaction và báo số transaction đã được ingress nhận vào
mempool; nó không chờ, ép hoặc dự đoán số block. Proposer tự chọn transaction
theo `MaxBlockTXs`/`MaxBlockBytes`, nên số block phụ thuộc tốc độ consensus,
network faults và mức đầy của từng proposal. Mặc định 20.000 transaction tương
đương khoảng 157 block nếu mọi block chứa đủ 128 transaction, nhưng đây chỉ là
ước lượng. Vì một sender chỉ được đi trước committed nonce tối đa 1024, script tự
thử lại cùng nonce khi mempool/cửa sổ nonce tạm đầy. Nếu ingress tắt hoặc không
hồi phục trong 60 giây cho một transaction, script dừng và báo lỗi thay vì lặp
vô hạn.

## Xem mempool và ledger khi node đang chạy

Mỗi node mở thêm một HTTP endpoint read-only trên loopback. Endpoint này không
tham gia consensus, không truyền private key và không cho phép sửa state. Snapshot
mempool được lấy qua owner event-loop của node nên không race với admission/prune;
ledger được đọc từ chính LevelDB đang mở của node.

Với config mới được tạo bởi launcher, `AdminAddress` của node 0..9 mặc định dùng
dải port 29020..29029 khi `--base-port` là 29000. Config cũ không có trường này
sẽ tự chọn một port localhost trống. Trong cả hai trường hợp, địa chỉ thật được
ghi tại `node-NN/admin` khi node sẵn sàng.

Cách dễ nhất là dùng script Python và chọn node muốn đọc:

```text
python tests/query_node.py --config-dir local-runs/test-delay --node 9 --view status
python tests/query_node.py --config-dir local-runs/test-delay --node 9 --view mempool
python tests/query_node.py --config-dir local-runs/test-delay --node 9 --view ledger
```

Ledger có thể được phân trang, ví dụ đọc tối đa 10 block bắt đầu từ height 101:

```text
python tests/query_node.py --config-dir local-runs/test-delay --node 9 --view ledger --from-height 101 --limit 10
```

Cũng có thể mở trực tiếp bằng browser hoặc HTTP client. Ví dụ nếu file `admin`
của node 9 chứa `127.0.0.1:29029`:

```text
http://127.0.0.1:29029/status
http://127.0.0.1:29029/mempool
http://127.0.0.1:29029/ledger?from=1&limit=20
```

`/mempool` trả số lượng, TransactionID và toàn bộ signed transaction đang chờ
trong RAM. `/ledger` trả height hiện tại và các finalized block/certificate đã
ghi trên disk. `/status` trả height, mempool size và state root. Kết quả là ảnh
chụp tại thời điểm request; ngay sau đó consensus có thể chuyển transaction từ
mempool vào block. Chỉ phương thức GET được chấp nhận. Endpoint bị giới hạn ở
loopback để phục vụ lab local; nó không phải public blockchain explorer.

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
