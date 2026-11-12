# Kiểm tra hiện tại: transaction mạng, mempool RAM, ledger LevelDB

Đây là ghi nhận thực thi test, không phải báo cáo nộp bài. Các stage note, report,
script đóng gói và snapshot log cũ đã bỏ vì mô tả kiến trúc không còn đúng.

## Kết quả đã thực thi

| Lượt chạy | Kết quả |
|---|---|
| local-runs/mempool-disk-normal-clean | Full normal sau sửa retry: exit 0, 222 test/subtest, T1-T8 và sáu behavior qua, formatting và vet qua |
| local-runs/mempool-disk-race-verified | Full race: exit 0, 222 test/subtest, không missing/failed case, không báo data race; formatting và vet qua |
| local-runs/mempool-race-retry-fix | Ba case race từng lỗi đã chạy lại và qua trước lượt full race |
| local-runs/mempool-manual | Production launcher: 10 process, node 0 crash, node 1 duplicate TX, client nộp hai TX thật qua node 9; tám honest node lưu cùng hai block/root trên disk |

Kiểm tra chạy từng terminal tại local-runs/mempool-terminal-entry cũng qua:
node honest rảnh không tự tạo transaction/vote; node crashed_proposer thoát mã 86
và launcher nhận diện đúng hành vi đã cấu hình.

Lệnh thực thi full suite từ thư mục project:

```text
python tests/run_tests.py --go D:/tools/golang/bin/go.exe --output local-runs/mempool-disk-normal-clean
python tests/run_tests.py --go D:/tools/golang/bin/go.exe --race --output local-runs/mempool-disk-race-verified
```

Runner chạy gofmt -l src, go test ./... -count=1 -json (thêm -race khi yêu cầu),
kiểm tra T1-T8 không bị bỏ sót và chạy go vet ./.... Kết quả gồm summary.json,
go-test.jsonl, go-vet.log và toàn bộ node config/event/LevelDB theo từng case.
222 là số tên test/subtest qua, gồm cả parent/subtest, không phải 222 mạng riêng.
TestLedgerWriterChild và TestProcessNodeEntry được skip ở parent, nhưng thực sự
chạy trong child process của các test tương ứng.

## T1-T8: điều kiện, fault, kết quả và invariant

Mỗi case dùng 10 process Go, n=10/f=3/quorum=7, key fixture public TEST-ONLY.
Transaction hợp lệ được client nộp qua RPC; honest peer nhận qua GossipSub và
mempool. Không preload transaction trong config. T7 chạy một height; case khác
chạy hai. Test đọc ledger từ LevelDB sau khi process đóng, thực thi lại từng
block, kiểm tra parent/body/root và certificate có >=7 signer hợp lệ khác nhau.

| ID | Điều kiện và input/fault | Kết quả quan sát qua | Invariant |
|---|---|---|---|
| T1 | Mạng honest, nộp nonce 1 rồi 2 | 10 node lưu cùng history/root; mempool_accept ở peer | Agreement và execution |
| T2 | 100% duplicate, hold 2-8ms và reorder định kỳ | Actual send của bản sao/reorder, duplicate vote bị từ chối, cùng hai block | Không double-count/sign/finalize |
| T3 | Proposer phát proposal sai chữ ký và vote sai domain | Cả chín honest peer ghi hai lý do reject và tiến ở round sau | Authentication không bị bypass |
| T4 | Proposer tạo body trùng nonce rồi replay candidate cũ | Reject nonce/height, chín replay send được kiểm tra; final TX đúng một lần | Atomic execution và replay protection |
| T5 | Drop mọi protocol publication/write trong 1200ms rồi ngừng fault | Không send/finalize trước hạn; sau đó lưu hai block đồng nhất | Safety trước ổn định, progress sau đó |
| T6 | Silent proposer và case riêng kill process proposer | Timeout/đổi round; chín survivor lưu hai block | Liveness với proposer đúng về sau |
| T7 | Ba process ký/phát A và B trong cả hai phase | Chữ ký xung đột thực sự được truyền; bảy honest node lưu cùng block | Safety tại giới hạn f |
| T8 | Hai run độc lập, cùng key/input/fault seed | Cùng signed TX order và expected root; cả hai có actual fault transmissions | Deterministic effects, không đòi timestamps giống nhau |

T5 chủ yếu kiểm tra loss hoàn toàn; delay khác zero được kiểm tra trong T2/T8.
Faulty process tự tạo/nhận message bằng key của chính nó; verifier không gán
ledger cho honest node. T7 chỉ bắt đầu sau khi TX client đến qua mạng.

## Mempool và disk

| Test | Initial/input | Quan sát và invariant |
|---|---|---|
| TestPoolOrderingReplayAndIsolation | Empty state; nhận nonce 3,1,2, duplicate; sửa buffer caller | Chờ nonce còn thiếu; chọn 1,2; giữ trước commit; prune sau commit; nonce 1 replay bị reject; nonce 3 còn chờ |
| TestPoolRejectsBadAndBoundedInputs | Chữ ký/chain/owner/nonce sai, count/byte bounds | Không admission hoặc selection vượt giới hạn; owner/foreign-chain case có chữ ký tương ứng hợp lệ |
| TestDurableSequentialLedger | Append height 1, duplicate/gap, close/reopen và foreign chain | Giữ nguyên body/height trên disk, không overwrite/gap, không mở sai chain |
| TestLedgerSurvivesProcessKill | Child Append Sync=true rồi parent kill mà không Close | Reopen LevelDB và đọc đúng body đã ghi; không dựa vào result.json/shutdown |
| TestDiskFailureCannotCommit | Inject lỗi disk khi đủ PRECOMMIT quorum | Lỗi được trả ra; height/state/ledger không commit và không emit Finalized |

Production node luôn truyền callback LevelDB vào consensus. In-memory engine
ledger chỉ phục vụ unit tests không khởi chạy node thật. Body đã final không
được giữ trong cache advertisement; matching body request đọc lại từ LevelDB.

## Behavior cấu hình thực tế

TestConfiguredBehaviors chạy sáu case độc lập với Behavior của node 0:
invalid_signature, wrong_domain, duplicate_transaction, replay_transaction,
silent_proposer, crashed_proposer. Mỗi case nộp TX qua mạng và yêu cầu chín
honest node có đúng hai block với expected execution/root/certificate. Bad/silent/
crashed proposer phải chuyển khỏi round 0; duplicate phải có reject evidence;
replay phải thực sự được kích hoạt. Không giảm quorum hoặc bỏ guard để tiến.

Gossip relay 0--1--2 không có link 0--2, bi-stream hai chiều, reconnect, membership,
header/body mismatch và temporary peer blocking vẫn được kiểm tra bằng socket.
Các unit crypto/reference bytes, state ownership/Merkle và consensus locks/NIL
tiếp tục nằm trong suite. Duplicate fast path chỉ áp dụng bản sao chính xác của
dữ liệu đã xác minh; nội dung/chữ ký đổi vẫn phải xác minh lại.

## Lỗi đã phát hiện và sửa

Lượt race đầu tại local-runs/mempool-disk-race-final qua T1-T8 nhưng ba behavior
chữ ký sai/domain sai/silent không final kịp deadline nộp nonce 2. Giữ lại raw
failure evidence. Log cho thấy retry vote round cũ và xác minh crypto lặp làm
chậm proposal/body mới dưới race instrumentation. Đã sửa để retry vote ở round
hiện tại (proof cũ vẫn nằm trong reproposal), tái sử dụng envelope do topic
validator đã xác thực, và reject exact duplicates bằng dữ liệu/hash đã xác minh.
Không đổi deadline, số block, fault policy, quorum, signature scope hay assertion
chống conflict. Ba focused race case sau đó qua và full race sau sửa qua.

Lượt normal tại local-runs/mempool-disk-normal-verified gặp một startup timeout
trong configured-invalid_signature, trước khi đủ mười process và trước consensus.
Chưa xác định nguyên nhân; không coi lượt đó là thành công. Case chạy riêng tại
local-runs/mempool-normal-startup-recheck qua với cùng deadline/assertion; lượt
full tiếp theo tại local-runs/mempool-disk-normal-clean qua đủ 222 test/subtest.
Giữ lại cả evidence lỗi để không che giấu hiện tượng khởi tạo không ổn định.

Sau lượt full, CLI được sửa để nhận diện cờ -submit ngay cả khi giá trị là chuỗi
rỗng. Thay đổi này chỉ chọn nhánh client; kiểm tra build/static và gọi CLI riêng
được thực hiện bổ sung, không thay đổi consensus hoặc các kết quả full ở trên.
Build, go test ./src/cmd/node (không có test riêng), gofmt -l src và go vet ./...
đều hoàn tất thành công. Gọi -submit= đến node đã dừng trả RPC Unavailable như
mong đợi, xác nhận đã vào nhánh client thay vì khởi chạy lại validator; đây không
phải kiểm tra admission thành công của một transaction rỗng.

## Giới hạn được công khai

Mempool và application state hiện tại ở RAM; finalized block/certificate ở disk.
Đọc lại block sau kill đã kiểm tra. Safe validator restart chưa có: phải persist
thêm vote/lock signing state trước khi cho phép ký sau khôi phục. Output cũ bị
từ chối và không bị overwrite. Full public-network deployment không được tuyên bố.
Fault controls giới hạn authored protocol traffic, không toàn bộ libp2p relay/
heartbeat hay kernel packet. Không có benchmark hiệu năng 4 MiB được bịa ra.
