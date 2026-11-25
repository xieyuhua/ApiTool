package store

import (
	"errors"
	"strings"
	"testing"
)

// TestUserStoreOptimisticLock 验证 RPC 整包保存路径的乐观锁：
// 同一用户两个会话（模拟两台设备），后保存者必须收到冲突而不是静默覆盖。
func TestUserStoreOptimisticLock(t *testing.T) {
	d := newTestDB(t)

	// 设备 A 首次写入，建立项目
	devA := NewUserStore(d, "u1", t.TempDir())
	dataA := devA.GetData()
	pid := dataA.CurrentProjectID
	dataA.Projects[0].Name = "项目A"
	if err := devA.SaveData(dataA); err != nil {
		t.Fatalf("A 首次保存: %v", err)
	}

	// 设备 B 读取同一份数据（此时记录版本）
	devB := NewUserStore(d, "u1", t.TempDir())
	dataB := devB.GetData()
	if dataB.CurrentProjectID != pid {
		t.Fatalf("B 应看到同一项目: %s vs %s", dataB.CurrentProjectID, pid)
	}

	// A 再保存一次（版本前进）
	dataA = devA.GetData()
	dataA.Projects[0].Name = "A的修改"
	if err := devA.SaveData(dataA); err != nil {
		t.Fatalf("A 第二次保存: %v", err)
	}

	// B 用陈旧版本（其 dataB 里的 Project.Version）保存 → 必须冲突
	dataB.Projects[0].Name = "B的修改"
	err := devB.SaveData(dataB)
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("B 的陈旧保存应返回冲突，实际: %v", err)
	}
	if !strings.Contains(ce.Msg, "已有更新版本") {
		t.Fatalf("冲突信息应可读: %s", ce.Msg)
	}

	// 服务端数据应保持 A 的修改（未被 B 覆盖）
	verify := NewUserStore(d, "u1", t.TempDir())
	got := verify.GetData()
	if got.Projects[0].Name != "A的修改" {
		t.Fatalf("A 的数据被覆盖了: %s", got.Projects[0].Name)
	}

	// B 重新读取后可正常保存
	dataB2 := devB.GetData()
	dataB2.Projects[0].Name = "B的新修改"
	if err := devB.SaveData(dataB2); err != nil {
		t.Fatalf("B 刷新后保存应成功: %v", err)
	}
	if got2 := verify.GetData(); got2.Projects[0].Name != "B的新修改" {
		t.Fatalf("B 刷新后保存未生效: %s", got2.Projects[0].Name)
	}
}
