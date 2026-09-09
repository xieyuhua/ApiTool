<template>
  <div class="pm">
    <!-- 隐藏的文件选择器：常驻渲染，供 SSH / SFTP 上传共用（修复 SSH 标签下点击上传无反应的问题） -->
    <input ref="fileInputRef" type="file" multiple style="position:absolute;width:0;height:0;opacity:0;left:-9999px" @change="onFilePicked" />
    <!-- 常驻标签栏：顶部，无标签时也显示 -->
    <div class="pm-tabs">
      <span v-if="!openTabs.length" class="pm-tabs-empty">未打开会话</span>
      <template v-for="tid in openTabs" :key="tid">
        <div class="pm-tab" :class="{ active: tid === activeTabConn }"
             @click="activeTabConn = tid" @click.middle.prevent="closeConnTab(tid)" :title="connOf(tid) ? connOf(tid).name : ''">
          <span class="pm-tab-ico">{{ connOf(tid) ? catIcon(connOf(tid).category) : '🔌' }}</span>
          <span class="pm-tab-name">{{ connOf(tid) ? connOf(tid).name : '(已删除)' }}</span>
          <span class="pm-tab-close" @click.stop="closeConnTab(tid)" title="关闭标签">×</span>
        </div>
      </template>
      <button class="pm-tab-plus" @click="selectorVisible = true" title="打开新标签 / 选择会话">＋</button>
    </div>

    <!-- 卡片网格（列表）：无任何打开标签时显示 -->
    <template v-if="!openTabs.length">
      <div class="pm-grid-head">
        <span class="pm-grid-title">{{ currentCat }}</span>
        <el-button size="small" type="primary" @click="openAdd">+ 新增连接</el-button>
      </div>
      <div class="pm-grid">
        <el-empty v-if="!categoryConns.length" :description="'暂无「' + currentCat + '」连接'">
          <el-button type="primary" size="small" @click="openAdd">新增连接</el-button>
        </el-empty>
        <div v-else class="pm-cards">
          <div v-for="c in categoryConns" :key="c.id" class="pm-card" :class="{ 'pm-card-active': c.category === 'db' && activeConnId === c.id }" @click="openConnTab(c.id)">
            <div class="pm-card-top">
              <span class="pm-card-ico">{{ catIcon(c.category) }}</span>
              <span class="pm-card-name" :title="c.name">{{ c.name }}</span>
              <span v-if="c.category === 'db' && activeConnId === c.id" class="pm-card-badge">分析中</span>
            </div>
            <div class="pm-card-host">{{ c.host || '—' }}<template v-if="c.port">:{{ c.port }}</template></div>
            <div class="pm-card-meta">{{ cardMeta(c) }}</div>
            <div class="pm-card-actions" @click.stop>
              <span class="pm-act" @click="editConn(c)">编辑</span>
              <span class="pm-act pm-act-del" @click="removeConn(c.id)">删除</span>
            </div>
          </div>
        </div>
      </div>
    </template>

    <!-- 详情 / 操作区（有激活标签时） -->
    <div class="pm-detail" v-else-if="selected" v-loading="loading">
      <div class="pm-detail-bar">
        <el-button size="small" @click="closeConnTab(activeTabConn)">← 关闭标签</el-button>
        <span class="pm-detail-title">{{ selected.name }}</span>
        <span class="pm-detail-tag">{{ currentCat }}</span>
        <span class="pm-detail-host">{{ selected.host }}<template v-if="selected.port">:{{ selected.port }}</template></span>
      </div>

      <!-- SSH：模仿 XShell 的实时交互终端 -->
      <div class="pm-body" v-if="selected.category === 'ssh'">
        <div class="term-bar">
          <span class="term-status" :class="{ on: sshActive.connected }">
            <i class="dot" />{{ sshActive.connected ? '已连接' : (sshActive.connecting ? '连接中…' : '未连接') }}
          </span>
          <span class="term-host">{{ selected.username ? selected.username + '@' : '' }}{{ selected.host }}<template v-if="selected.port">:{{ selected.port }}</template></span>
          <span style="flex:1" />
          <el-button size="small" type="danger" plain :disabled="!sshActive.connected" @click="closeSshActive">断开</el-button>
          <el-button size="small" type="success" @click="pickUpload(uploadToSsh)" title="rz：上传文件到远端当前目录（也可在终端直接输入 rz）">rz 上传</el-button>
          <el-button size="small" type="warning" @click="openSzDialog" title="sz：从远端下载文件（也可在终端直接输入 sz <路径>）">sz 下载</el-button>
          <el-button size="small" @click="clearSshLog">清屏</el-button>
        </div>

        <!-- xterm.js 终端容器：负责渲染输出与捕获键盘输入（按标签 key 隔离，避免多标签 DOM 复用冲突） -->
        <div class="term" ref="termRef" :key="activeTabConn" @click="focusTermInput">
          <div v-if="!sshActive.connected" class="term-overlay">
            <el-button type="primary" size="small" :loading="sshActive.connecting" @click="openSsh">连接</el-button>
            <span class="term-hint">点击「连接」打开实时终端会话</span>
          </div>
        </div>
      </div>

      <!-- SFTP / FTP -->
      <div class="pm-body" v-else-if="selected.category === 'sftp' || selected.category === 'ftp'">
        <div class="pm-line pm-wrap">
          <el-button size="small" @click="gotoParent">返回上级</el-button>
          <el-button size="small" type="primary" @click="listRemote">刷新</el-button>
          <el-button size="small" @click="remoteMkdirShown = true">新建目录</el-button>
          <el-button size="small" type="success" @click="pickUpload(uploadToRemote)">上传文件</el-button>
          <el-button size="small" type="danger" plain :disabled="!remoteChecked.length" @click="remoteDeleteBatch">
            批量删除<template v-if="remoteChecked.length">（{{ remoteChecked.length }}）</template>
          </el-button>
          <span class="pm-path">
            <span v-for="(seg, i) in pathSegments" :key="i" class="pm-path-seg" @click="gotoSeg(seg)">{{ seg.name }}<span v-if="i < pathSegments.length - 1"> / </span></span>
          </span>
        </div>
        <div class="pm-tbl-grow">
          <el-table :data="remoteFiles" size="small" height="100%" @row-dblclick="onRemoteRowDblclick"
                    @selection-change="v => remoteChecked = v">
            <el-table-column type="selection" width="40" />
            <el-table-column label="名称">
              <template #default="{ row }">
                <span>{{ row.isDir ? '📁' : '📄' }} {{ row.name }}</span>
              </template>
            </el-table-column>
            <el-table-column label="大小" width="110">
              <template #default="{ row }">{{ row.isDir ? '-' : fmtSize(row.size) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="290">
              <template #default="{ row }">
                <el-button v-if="row.isDir" size="small" @click="gotoSeg({ path: row.path })">进入</el-button>
                <el-button v-else size="small" @click="remoteRead(row)">查看</el-button>
                <el-button v-if="!row.isDir" size="small" type="primary" plain @click="remoteDownload(row)">下载</el-button>
                <el-button size="small" @click="openRename(row)">重命名</el-button>
                <el-button size="small" type="danger" plain @click="remoteDelete(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div class="pm-sub">文件内容 / 编辑（{{ currentRemotePath || '未选择' }}）</div>
        <el-input v-model="remoteContent" type="textarea" class="pm-content" placeholder="选择文件后查看或编辑" />
        <el-button size="small" type="primary" class="pm-sep" @click="remoteWrite" :disabled="!currentRemotePath">保存写入</el-button>

        <el-dialog v-model="remoteMkdirShown" title="新建目录" width="360px">
          <el-input v-model="remoteMkdirName" placeholder="目录名（相对当前路径）" />
          <template #footer>
            <el-button @click="remoteMkdirShown = false">取消</el-button>
            <el-button type="primary" @click="remoteMkdir">确定</el-button>
          </template>
        </el-dialog>

        <el-dialog v-model="renameShown" title="重命名" width="380px">
          <el-input v-model="renameName" placeholder="新名称" @keyup.enter="doRename" />
          <div style="margin-top:6px;color:#86909c;font-size:12px">原名称：{{ renameRow && renameRow.name }}</div>
          <template #footer>
            <el-button @click="renameShown = false">取消</el-button>
            <el-button type="primary" @click="doRename">确定</el-button>
          </template>
        </el-dialog>
      </div>

      <!-- 数据库连接：测试连接 + 启用分析 + 同步表结构 / 字段语义维护 -->
      <div class="pm-body pm-db" v-else-if="selected.category === 'db'">
        <div class="pm-line pm-wrap">
          <el-button size="small" type="success" @click="dbTest">测试连接</el-button>
          <span v-if="testResult" :class="['pm-test', testResult.ok ? 'ok' : 'err']">
            {{ testResult.ok ? '✓ ' + (testResult.info || '连接成功') : '✗ ' + (testResult.error || '连接失败') }}
          </span>
          <span class="spacer" />
          <el-switch
            :model-value="agentCfg.activeDBConn === selected.id"
            @change="setActiveConn"
            active-text="启用此连接做数据分析（同时仅一个）"
          />
        </div>
        <el-descriptions :column="2" border size="small" class="pm-meta">
          <el-descriptions-item label="类型">{{ (selected.dbType || 'mysql').toUpperCase() }}</el-descriptions-item>
          <el-descriptions-item label="主机">{{ selected.host }}<template v-if="selected.port">:{{ selected.port }}</template></el-descriptions-item>
          <el-descriptions-item label="用户名">{{ selected.username || '—' }}</el-descriptions-item>
          <el-descriptions-item label="默认库/Schema">{{ selected.database || '—' }}</el-descriptions-item>
          <el-descriptions-item label="备注" :span="2">{{ selected.remark || '—' }}</el-descriptions-item>
        </el-descriptions>
        <div v-if="agentCfg.activeDBConn === selected.id" class="pm-active-banner">
          ✓ 当前分析连接：Agent 执行 db_schema / db_query 时将使用此连接（同一时间仅一个）
        </div>

        <el-card class="db-mgr-card" shadow="never">
          <template #header>
            <div class="db-mgr-head">
              <span class="db-mgr-title">表结构同步 / 字段语义维护</span>
              <div class="db-mgr-tools">
                <span class="db-autosave">修改后自动保存</span>
                <el-select v-model="selDatabase" size="small" placeholder="选择数据库 / Schema" style="width:220px" filterable @change="onDbDatabaseChange">
                  <el-option v-for="d in dbDatabases" :key="d" :label="d" :value="d" />
                </el-select>
                <el-button size="small" type="success" :loading="syncing" :disabled="!selTables.length" @click="syncTableSchema">同步选中表结构</el-button>
              </div>
            </div>
          </template>

          <div v-if="!selDatabase" class="pm-hint pm-block">请先在右上角选择一个数据库 / Schema，再勾选表同步结构、维护字段语义。数据将与「AI Agent → 数据连接」共享。</div>

          <div v-else class="db-mgr-body">
            <!-- 左侧：待同步表 -->
            <div class="db-pick">
              <div class="db-pick-bar">
                <span>待同步表（{{ dbTables.length }}）</span>
                <el-input v-model="tblFilter" size="small" placeholder="搜索表名" clearable style="width:150px" />
              </div>
              <div class="table-pick">
                <el-checkbox-group v-model="selTables" class="tp-list">
                  <el-checkbox v-for="t in filteredTables" :key="t.name" :value="t.name" border size="small"
                    :disabled="!!agentCfg.dbSchemas && !!agentCfg.dbSchemas[dbKey(selected.id, selDatabase, t.name)]">
                    {{ t.name }}<span class="tp-rows" v-if="t.rows"> ({{ t.rows }})</span>
                    <span v-if="agentCfg.dbSchemas && agentCfg.dbSchemas[dbKey(selected.id, selDatabase, t.name)]" class="tp-synced">已同步</span>
                  </el-checkbox>
                </el-checkbox-group>
                <div v-if="!filteredTables.length" class="empty-hint">没有匹配的表。</div>
              </div>
            </div>

            <!-- 右侧：已同步表结构（表格化，展开看字段） -->
            <div class="db-synced">
              <div class="db-pick-bar">
                <span>已同步表结构（{{ syncedTables.length }}）</span>
                <el-input v-model="syncedFilter" size="small" placeholder="搜索表名" clearable style="width:150px" />
              </div>
              <el-table v-if="syncedTables.length" :data="filteredSynced" size="small" border class="synced-table" max-height="460" row-key="table">
                <el-table-column type="expand">
                  <template #default="{ row }">
                    <div class="table-sem-inline">
                      <span class="sem-col sm">表语义</span>
                      <el-input :model-value="tableSemValue(row.connId, row.database, row.table)"
                        @update:model-value="setTableSemValue(row.connId, row.database, row.table, $event)"
                        size="small" placeholder="维护此表的整体语义（如：订单主表，记录用户下单信息）" />
                      <el-button size="small" type="primary" plain :loading="genAllLoading === row.table"
                        @click="genSemanticAll(row.connId, row.database, row.table, row.columns)">AI 生成全部</el-button>
                    </div>
                    <el-table :data="row.columns || []" size="small" border class="col-table">
                      <el-table-column prop="name" label="字段名" min-width="140" />
                      <el-table-column prop="type" label="类型" min-width="110" />
                      <el-table-column prop="comment" label="库注释" min-width="130" show-overflow-tooltip />
                      <el-table-column label="中文语义（维护）" min-width="300">
                        <template #default="{ row: c }">
                          <div class="sem-edit">
                            <el-input :model-value="semValue(row.connId, row.database, row.table, c.name)"
                              @update:model-value="setSemValue(row.connId, row.database, row.table, c.name, $event)"
                              size="small" placeholder="如：订单创建时间" />
                          </div>
                        </template>
                       </el-table-column>
                      <el-table-column label="操作" width="80" align="center" fixed="right">
                        <template #default="{ row: c }">
                          <el-button size="small" text type="danger" @click="removeField(row, c)">删除</el-button>
                        </template>
                      </el-table-column>
                    </el-table>
                  </template>
                </el-table-column>
                <el-table-column prop="table" label="表名" min-width="180" />
                <el-table-column label="字段数" width="90" align="center">
                  <template #default="{ row }">{{ (row.columns || []).length }}</template>
                </el-table-column>
                <el-table-column label="表语义" min-width="200" show-overflow-tooltip>
                  <template #default="{ row }">
                    <span v-if="tableSemValue(row.connId, row.database, row.table)">{{ tableSemValue(row.connId, row.database, row.table) }}</span>
                    <span v-else class="muted">未维护</span>
                  </template>
                </el-table-column>
                <el-table-column label="操作" width="100" align="center" fixed="right">
                  <template #default="{ row }">
                    <el-button size="small" text type="danger" @click="removeSynced(row)">移除</el-button>
                  </template>
                </el-table-column>
              </el-table>
              <div v-else class="empty-hint">尚未同步任何表结构。在左侧勾选表后点「同步选中表结构」。</div>
              <div v-if="syncedTables.length && !filteredSynced.length" class="empty-hint">没有匹配的已同步表。</div>
            </div>
          </div>
        </el-card>
      </div>

      <!-- 兜底：未知分类 -->
      <div class="pm-body" v-else>
        <el-empty description="该连接类型暂不支持操作面板" />
      </div>
    </div>

    <!-- 新增/编辑连接 -->
    <el-dialog v-model="showAdd" :title="editing ? '编辑连接' : '新增连接'" width="440px">
      <el-form label-width="90px" size="small">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="分类">
          <el-select v-model="form.category" style="width:100%">
            <el-option label="XShell(SSH)" value="ssh" />
            <el-option label="FTP" value="ftp" />
            <el-option label="SFTP" value="sftp" />
            <el-option label="数据库连接(MySQL/PG/Oracle)" value="db" />
          </el-select>
        </el-form-item>
        <el-form-item label="编码" v-if="form.category === 'ssh'">
          <el-select v-model="form.encoding" style="width:100%">
            <el-option label="UTF-8（默认）" value="utf-8" />
            <el-option label="GBK（中文服务器常见）" value="gbk" />
            <el-option label="GB18030" value="gb18030" />
          </el-select>
        </el-form-item>
        <el-form-item label="数据库类型" v-if="form.category === 'db'">
          <el-select v-model="form.dbType" style="width:100%">
            <el-option label="MySQL" value="mysql" />
            <el-option label="PostgreSQL" value="postgres" />
            <el-option label="Oracle" value="oracle" />
          </el-select>
        </el-form-item>
        <el-form-item label="主机"><el-input v-model="form.host" /></el-form-item>
        <el-form-item label="端口"><el-input v-model="form.port" /></el-form-item>
        <el-form-item label="默认库/Schema" v-if="form.category === 'db'"><el-input v-model="form.database" placeholder="mysql: 库名；pg/oracle: schema" /></el-form-item>
        <el-form-item label="用户名"><el-input v-model="form.username" /></el-form-item>
        <el-form-item label="密码"><el-input v-model="form.password" type="password" show-password /></el-form-item>
        <el-form-item label="备注"><el-input v-model="form.remark" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAdd = false">取消</el-button>
        <el-button type="primary" @click="saveConn">保存</el-button>
        <el-button type="success" plain @click="testConn">测试连接</el-button>
      </template>
      <div v-if="testResult" style="margin-top:8px" :style="{ color: testResult.ok ? '#67c23a' : '#f56c6c' }">
        {{ testResult.ok ? (testResult.info || '连接成功') : ('失败：' + (testResult.error || '未知错误（请检查连接参数）')) }}
      </div>
    </el-dialog>

    <!-- sz 下载：输入远端文件路径，保存到本地（等效 XShell 中 sz） -->
    <el-dialog v-model="szDialogVisible" title="sz 下载文件" width="440px" append-to-body>
      <div style="display:flex;flex-direction:column;gap:8px">
        <span style="font-size:12px;color:#86909c">输入远端文件的绝对路径，确认后将通过 SFTP 安全下载并保存到本地。</span>
        <el-input v-model="szRemotePath" placeholder="例如：/tmp/example.tar.gz" @keyup.enter="confirmSz">
          <template #prepend>远端路径</template>
        </el-input>
      </div>
      <template #footer>
        <el-button @click="szDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmSz">下载</el-button>
      </template>
    </el-dialog>

    <!-- 会话选择器：点击标签栏「＋」弹出，列出各分类已有连接，点选即开新标签 -->
    <el-dialog v-model="selectorVisible" title="打开会话（新标签）" width="600px" append-to-body>
      <div class="sel-groups">
        <div v-for="g in selectorGroups" :key="g.value" class="sel-group">
          <div class="sel-group-title">{{ g.ico }} {{ g.label }}</div>
          <div v-if="!g.items.length" class="sel-empty">暂无连接</div>
          <div v-else class="sel-items">
            <div v-for="c in g.items" :key="c.id" class="sel-item" @click="openConnTab(c.id)">
              <span class="sel-ico">{{ g.ico }}</span>
              <span class="sel-name" :title="c.name">{{ c.name }}</span>
              <span class="sel-host">{{ c.host || '—' }}<template v-if="c.port">:{{ c.port }}</template></span>
              <span class="sel-open">打开 ↗</span>
            </div>
          </div>
        </div>
      </div>
      <template #footer>
        <div class="sel-footer">
          <el-dropdown @command="onNewConnType" trigger="click">
            <el-button size="small" type="primary">＋ 新建连接 ▾</el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="ssh">XShell(SSH)</el-dropdown-item>
                <el-dropdown-item command="ftp">FTP</el-dropdown-item>
                <el-dropdown-item command="sftp">SFTP</el-dropdown-item>
                <el-dropdown-item command="db">数据库连接</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  PluginTest,
  PluginSSHOpen, PluginSSHInput, PluginSSHClose, PluginSSHResize, PluginSSHExec,
  PluginSFTPList, PluginSFTPRead, PluginSFTPWrite, PluginSFTPMkdir, PluginSFTPDelete,
  PluginSFTPUploadB64, PluginSFTPRename, PluginSFTPDownload,
  PluginFTPList, PluginFTPRead, PluginFTPWrite, PluginFTPMkdir, PluginFTPDelete,
  PluginFTPUploadB64, PluginFTPRename, PluginFTPDownload,
  PluginDBDatabases, PluginDBTables, PluginDBColumns,
} from '../../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime'
// 标准终端引擎：完整支持 ANSI/VT100、真彩色、交替屏，能正确渲染 htop/vim/top 等全屏 TUI
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { pluginConnections, addPluginConn, updatePluginConn, removePluginConn, callAI } from '../../store.js'
import { AgentAPI } from '../agent/agentApi'

// 当前激活用于数据分析的数据库连接 ID（用于卡片/详情展示徽标）
const activeConnId = ref('')
async function loadActiveConnId() {
  try {
    const d = await AgentAPI.load()
    activeConnId.value = (d.config && d.config.activeDBConn) || ''
  } catch (e) { activeConnId.value = '' }
}
loadActiveConnId()

// 连接分类定义
const categories = [
  { value: 'ssh', label: 'XShell(SSH)', ico: '💻' },
  { value: 'ftp', label: 'FTP', ico: '📁' },
  { value: 'sftp', label: 'SFTP', ico: '📂' },
  { value: 'db', label: '数据库连接', ico: '🗄️' },
]
const catIconMap = { ssh: '💻', ftp: '📁', sftp: '📂', db: '🗄️' }

// Props：当前选中的分类（由 Tools.vue 左侧导航传入）
const props = defineProps({ category: { type: String, default: 'ssh' } })

// 视图状态
const loading = ref(false)
const showAdd = ref(false)
const editing = ref(false)
const editingId = ref('')
const testResult = ref(null)

// 表单状态（新增/编辑连接）
const form = reactive({ name: '', category: 'ssh', dbType: 'mysql', host: '', port: 0, username: '', password: '', database: '', remark: '', encoding: 'utf-8' })

// ===================== 多标签（类浏览器标签） =====================
const openTabs = ref([])          // 已打开的标签（连接 id 数组，可跨分类）
const activeTabConn = ref('')     // 当前激活标签对应的连接 id
const tabCache = {}               // connId -> 非 SSH 标签的视图态快照（切换标签时持久化）
const sshStatus = reactive({})    // connId -> { connected, connecting, sessionId }（响应式，供标签栏状态展示）
const sshEngines = {}             // connId -> { term, fitAddon, ro }（非响应式，避免 xterm 被代理）
const selectorVisible = ref(false)// 会话选择器对话框

const termRef = ref(null)         // 当前激活 SSH 标签的终端容器 DOM
const remotePath = ref('/')
const remoteFiles = ref([])
const remoteContent = ref('')
const currentRemotePath = ref('')
const remoteMkdirShown = ref(false)
const remoteMkdirName = ref('')
const remoteChecked = ref([])      // 表格多选中的文件行（批量删除用）
const renameShown = ref(false)     // 重命名弹窗
const renameRow = ref(null)        // 待重命名的行
const renameName = ref('')
const fileInputRef = ref(null)
let uploadHandler = null  // 当前上传目标处理回调

// 当前分类标题
const currentCat = computed(() => (categories.find(c => c.value === props.category) || {}).label || '')
// 当前分类下的连接列表（左侧卡片网格用，按导航分类过滤）
const categoryConns = computed(() => pluginConnections().filter(c => c.category === props.category))
// 当前激活标签对应的连接对象（跨分类查找）
const selected = computed(() => pluginConnections().find(c => c.id === activeTabConn.value))
// 标签栏展示用：根据 id 取连接
function connOf(id) { return pluginConnections().find(c => c.id === id) || null }
// 会话选择器分组：按分类列出已有连接
const selectorGroups = computed(() => categories.map(cat => ({
  value: cat.value, label: cat.label, ico: cat.ico,
  items: pluginConnections().filter(c => c.category === cat.value),
})))
// 当前激活 SSH 标签的实时状态（供模板展示连接/连接中）
const sshActive = computed(() => {
  const id = activeTabConn.value
  if (!id) return { connected: false, connecting: false }
  const s = sshStatus[id]
  return s ? { connected: !!s.connected, connecting: !!s.connecting } : { connected: false, connecting: false }
})

// 切换左侧分类导航仅影响卡片网格过滤；已打开的标签保持不变（组件实例复用，不重置）

// 分类图标
function catIcon(cat) { return catIconMap[cat] || '🔌' }
// 卡片副标题信息
function cardMeta(c) {
  switch (c.category) {
    case 'ssh': return c.username ? '用户 ' + c.username : '远程终端'
    case 'sftp':
    case 'ftp': return c.username ? '用户 ' + c.username : '文件传输'
    case 'db': return (c.dbType || 'mysql').toUpperCase() + (c.database ? ' · ' + c.database : '')
    default: return c.remark || ''
  }
}

// 打开一个连接为新标签（已打开则直接切换）
function openConnTab(id) {
  if (!id) return
  if (!openTabs.value.includes(id)) openTabs.value.push(id)
  activeTabConn.value = id
  selectorVisible.value = false
}

// 路径分段，用于面包屑导航
const pathSegments = computed(() => {
  const p = remotePath.value || '/'
  if (p === '/' || p === '') return [{ name: '根目录', path: '/' }]
  const parts = p.split('/').filter(Boolean)
  const segs = [{ name: '根', path: '/' }]
  let acc = ''
  for (const part of parts) {
    acc += '/' + part
    segs.push({ name: part, path: acc })
  }
  return segs
})

// 统一调用包装：处理 loading 与错误提示
async function call(fn, ...args) {
  loading.value = true
  try {
    const r = await fn(...args)
    // 后端 PluginOpResult 的 JSON 字段为小写：ok / error / info
    if (r && r.ok === false) ElMessage.error(r.error || '操作失败')
    return r
  } catch (e) {
    ElMessage.error((e && e.message) ? e.message : String(e))
    return null
  } finally {
    loading.value = false
  }
}

// 重置非 SSH 标签的视图态（打开新标签且无可恢复快照时调用）
function resetView() {
  remotePath.value = '/'
  remoteFiles.value = []
  remoteContent.value = ''
  currentRemotePath.value = ''
  remoteMkdirShown.value = false
  remoteMkdirName.value = ''
  remoteChecked.value = []
  renameShown.value = false
  renameRow.value = null
  renameName.value = ''
  dbDatabases.value = []
  dbTables.value = []
  selDatabase.value = ''
  selTables.value = []
  tblFilter.value = ''
  syncedFilter.value = ''
  expandedTables.value = new Set()
}

// 切换激活标签：保存旧标签视图态、恢复/加载新标签，并接管对应连接的操作区
function saveCache(id) {
  if (!id) return
  tabCache[id] = {
    remotePath: remotePath.value, remoteFiles: [...remoteFiles.value],
    remoteContent: remoteContent.value, currentRemotePath: currentRemotePath.value,
    remoteChecked: [...remoteChecked.value], remoteMkdirShown: remoteMkdirShown.value,
    remoteMkdirName: remoteMkdirName.value, renameShown: renameShown.value,
    renameRow: renameRow.value, renameName: renameName.value,
    dbDatabases: [...dbDatabases.value], dbTables: [...dbTables.value],
    selDatabase: selDatabase.value, selTables: [...selTables.value],
    tblFilter: tblFilter.value, syncedFilter: syncedFilter.value,
    expandedTables: new Set(expandedTables.value),
  }
}
function restoreView(c) {
  remotePath.value = c.remotePath ?? '/'
  remoteFiles.value = c.remoteFiles ?? []
  remoteContent.value = c.remoteContent ?? ''
  currentRemotePath.value = c.currentRemotePath ?? ''
  remoteChecked.value = c.remoteChecked ?? []
  remoteMkdirShown.value = c.remoteMkdirShown ?? false
  remoteMkdirName.value = c.remoteMkdirName ?? ''
  renameShown.value = c.renameShown ?? false
  renameRow.value = c.renameRow ?? null
  renameName.value = c.renameName ?? ''
  dbDatabases.value = c.dbDatabases ?? []
  dbTables.value = c.dbTables ?? []
  selDatabase.value = c.selDatabase ?? ''
  selTables.value = c.selTables ?? []
  tblFilter.value = c.tblFilter ?? ''
  syncedFilter.value = c.syncedFilter ?? ''
  expandedTables.value = c.expandedTables ? new Set(c.expandedTables) : new Set()
}
// 关闭标签：释放 SSH 资源、清理缓存
function closeConnTab(id) {
  const idx = openTabs.value.findIndex(x => x === id)
  if (idx === -1) return
  disposeSsh(id)
  delete tabCache[id]
  openTabs.value.splice(idx, 1)
  if (activeTabConn.value === id) {
    const next = openTabs.value[idx] || openTabs.value[idx - 1] || ''
    activeTabConn.value = next
  }
}
// 激活标签时按需加载：SSH 自动建立/恢复实时会话，文件类自动列目录，DB 类加载分析配置
watch(activeTabConn, async (newId, oldId) => {
  if (oldId) saveCache(oldId)
  if (!newId) { resetView(); return }
  const conn = connOf(newId)
  if (!conn) return
  const cached = tabCache[newId]
  if (cached) restoreView(cached)
  else resetView()
  if (conn.category === 'ssh') await openSsh()
  else if (conn.category === 'sftp' || conn.category === 'ftp') { if (!cached) await listRemote() }
  else if (conn.category === 'db') { if (!cached) await openDbConn(conn); else await loadAgentCfg() }
})

async function openDbConn(val) {
  selTables.value = []
  dbDatabases.value = []
  dbTables.value = []
  tblFilter.value = ''
  syncedFilter.value = ''
  expandedTables.value = new Set()
  await loadAgentCfg()
  activeConnId.value = agentCfg.activeDBConn
  await loadDbDatabases()
  // 恢复上次在该连接中选中的库（若仍存在）
  const lastDB = (agentCfg.dbLastDB && agentCfg.dbLastDB[val.id]) || ''
  if (lastDB && dbDatabases.value.includes(lastDB)) {
    selDatabase.value = lastDB
    await loadDbTables(val, lastDB)
  } else {
    selDatabase.value = ''
  }
}
async function loadDbDatabases() {
  try {
    const dbs = await call(PluginDBDatabases, selected.value)
    const list = Array.isArray(dbs) ? dbs : (dbs && dbs.databases) || []
    dbDatabases.value = (list || []).map(d => (typeof d === 'string' ? d : d.name ?? d))
  } catch (e) { dbDatabases.value = [] }
}
async function loadDbTables(conn, database) {
  dbTables.value = []
  if (!database) return
  const tabs = await call(PluginDBTables, conn, database)
  const tabList = Array.isArray(tabs) ? tabs : (tabs && tabs.tables) || []
  dbTables.value = (tabList || []).map(t => ({ name: t.name ?? t, rows: t.rows ?? 0 }))
}

// ===================== 连接 CRUD =====================
function blankForm() {
  Object.assign(form, {
    name: '', category: props.category, dbType: 'mysql', host: '', port: 0,
    username: '', password: '', database: '', remark: '', encoding: 'utf-8',
  })
}
function openAdd() { editing.value = false; editingId.value = ''; testResult.value = null; blankForm(); showAdd.value = true }
// 从会话选择器进入「新增连接」表单，并预选分类
function onNewConnType(type) {
  selectorVisible.value = false
  editing.value = false
  editingId.value = ''
  testResult.value = null
  blankForm()
  form.category = type
  showAdd.value = true
}
function editConn(c) {
  editing.value = true; editingId.value = c.id; testResult.value = null
  Object.assign(form, { name: c.name, category: c.category, dbType: c.dbType || 'mysql', host: c.host, port: c.port,
    username: c.username, password: c.password, database: c.database || '', remark: c.remark, encoding: c.encoding || 'utf-8' })
  showAdd.value = true
}
function saveConn() {
  if (!form.name) { ElMessage.error('请填写名称'); return }
  if (!form.host) { ElMessage.error('请填写主机'); return }
  const conn = {
    id: editing.value ? editingId.value : ('pl_' + Date.now()),
    category: form.category, dbType: form.dbType, name: form.name, host: form.host,
    port: Number(form.port) || 0, username: form.username, password: form.password,
    database: form.database, remark: form.remark, encoding: form.encoding,
    updatedAt: new Date().toISOString(),
  }
  if (editing.value) updatePluginConn(conn)
  else addPluginConn(conn)
  showAdd.value = false
  ElMessage.success('已保存')
}
async function testConn() {
  const conn = {
    id: editingId.value || 'test', category: form.category, dbType: form.dbType, name: form.name, host: form.host,
    port: Number(form.port) || 0, username: form.username, password: form.password, database: form.database,
  }
  testResult.value = await call(PluginTest, conn)
}
// 详情面板中测试连接（复用 testConn 的取参逻辑）
async function dbTest() {
  const c = selected.value
  if (!c) return
  testResult.value = await call(PluginTest, c)
}

// ===================== 数据库连接的「同步表结构 / 字段语义维护」 =====================
// 与 AI Agent → 数据连接 共享同一份 agent 配置（activeDBConn / dbSchemas / dbSemantics）
const agentCfg = reactive({ activeDBConn: '', dbSchemas: {}, dbSemantics: {} })
const dbDatabases = ref([])
const dbTables = ref([])
const selDatabase = ref('')
const selTables = ref([])
const syncing = ref(false)
const tblFilter = ref('')
const syncedFilter = ref('')
const expandedTables = ref(new Set())

const filteredTables = computed(() => {
  const q = tblFilter.value.trim().toLowerCase()
  if (!q) return dbTables.value
  return dbTables.value.filter(t => (t.name || '').toLowerCase().includes(q))
})
const syncedTables = computed(() => {
  const prefix = (selected.value.id + '|' + selDatabase.value + '|').toLowerCase()
  return Object.keys(agentCfg.dbSchemas || {})
    .filter(k => k.startsWith(prefix))
    .map(k => agentCfg.dbSchemas[k])
    .sort((a, b) => a.table.localeCompare(b.table))
})
const filteredSynced = computed(() => {
  const q = syncedFilter.value.trim().toLowerCase()
  if (!q) return syncedTables.value
  return syncedTables.value.filter(t => (t.table || '').toLowerCase().includes(q))
})
function dbKey(connId, database, table) {
  return (connId + '|' + database + '|' + table).toLowerCase()
}
function semKey(connId, database, table, col) {
  return (connId + '|' + database + '|' + table + '|' + col).toLowerCase()
}
function tableSemKey(connId, database, table) {
  return (connId + '|' + database + '|' + table).toLowerCase()
}
function columnsOf(t) {
  if (!t) return []
  if (Array.isArray(t.columns)) return t.columns
  const s = agentCfg.dbSchemas[dbKey(t.connId, t.database, t.table)]
  return s ? (s.columns || []) : []
}
function semValue(connId, database, table, col) {
  return (agentCfg.dbSemantics && agentCfg.dbSemantics[semKey(connId, database, table, col)]) || ''
}
function setSemValue(connId, database, table, col, v) {
  if (!agentCfg.dbSemantics) agentCfg.dbSemantics = {}
  const k = semKey(connId, database, table, col)
  if (v && v.trim()) agentCfg.dbSemantics[k] = v.trim()
  else delete agentCfg.dbSemantics[k]
  debouncedSave()
}
function tableSemValue(connId, database, table) {
  return (agentCfg.dbSemantics && agentCfg.dbSemantics[tableSemKey(connId, database, table)]) || ''
}
function setTableSemValue(connId, database, table, v) {
  if (!agentCfg.dbSemantics) agentCfg.dbSemantics = {}
  const k = tableSemKey(connId, database, table)
  if (v && v.trim()) agentCfg.dbSemantics[k] = v.trim()
  else delete agentCfg.dbSemantics[k]
  debouncedSave()
}
// AI 一键生成语义：根据表/字段名、类型、已有注释，让模型生成简洁中文业务描述
const genAllLoading = ref('') // 正在批量生成整张表语义的表名
// 删除单个字段：从已同步表结构中移除该列，并清除其语义
function removeField(t, col) {
  if (!t || !col) return
  const k = dbKey(t.connId, t.database, t.table)
  const s = agentCfg.dbSchemas[k]
  if (!s || !Array.isArray(s.columns)) return
  const idx = s.columns.findIndex(c => c.name === col.name)
  if (idx < 0) return
  s.columns.splice(idx, 1)
  if (agentCfg.dbSemantics) delete agentCfg.dbSemantics[semKey(t.connId, t.database, t.table, col.name)]
  saveAgentCfg()
  ElMessage.success('已删除字段「' + col.name + '」')
}
// 批量 AI 生成：一次生成整张表的表语义 + 所有字段语义
async function genSemanticAll(connId, database, table, columns) {
  if (genAllLoading.value) return
  genAllLoading.value = table
  try {
    const dbType = (selected.value && selected.value.dbType) || '未知'
    const colList = Array.isArray(columns) ? columns : []
    const colLines = colList.map((c, i) =>
      (i + 1) + '. 字段名=' + c.name + '，类型=' + (c.type || '未知') + '，库注释=' + (c.comment && c.comment.trim() ? c.comment.trim() : '无')
    ).join('\n')
    const sys = '你是一名资深数据架构师与业务分析师。请为给定的数据库表生成语义说明。必须只返回一个 JSON 对象（不要任何解释、不要 markdown 代码块），结构为：{"tableSem":"整张表的业务含义（一句话，不超过40字）","fields":{"字段名":"该字段的中文业务描述（一句话，不超过30字）"}}。字段名必须与输入完全一致。'
    const user = [
      '数据库类型：' + dbType,
      '库名：' + database,
      '表名：' + table,
      '字段列表（共 ' + colList.length + ' 个）：',
      colLines || '（无字段）',
    ].join('\n')
    const out = await callAI([
      { role: 'system', content: sys },
      { role: 'user', content: user },
    ], { maxTokens: 1500 })
    const text = (out || '').trim()
    if (!text) { ElMessage.warning('AI 未返回结果'); return }
    // 兼容模型可能包裹 ```json ... ``` 的情况
    let jsonStr = text
    const fence = text.match(/```(?:json)?\s*([\s\S]*?)```/i)
    if (fence) jsonStr = fence[1].trim()
    let parsed
    try { parsed = JSON.parse(jsonStr) } catch (e) { ElMessage.error('AI 返回格式无法解析，请重试'); return }
    let n = 0
    if (parsed && parsed.tableSem && parsed.tableSem.trim()) {
      setTableSemValue(connId, database, table, parsed.tableSem.trim()); n++
    }
    if (parsed && parsed.fields && typeof parsed.fields === 'object') {
      for (const c of colList) {
        const v = parsed.fields[c.name]
        if (v && v.trim()) { setSemValue(connId, database, table, c.name, v.trim()); n++ }
      }
    }
    if (n > 0) ElMessage.success('已批量生成 ' + n + ' 条语义')
    else ElMessage.warning('未解析到可写入的语义')
  } catch (e) {
    ElMessage.error('批量生成失败：' + String(e))
  } finally {
    genAllLoading.value = ''
  }
}
function toggleExpand(table) {
  const s = new Set(expandedTables.value)
  if (s.has(table)) s.delete(table); else s.add(table)
  expandedTables.value = s
}
async function loadAgentCfg() {
  try {
    const d = await AgentAPI.load()
    const cfg = d.config || {}
    agentCfg.activeDBConn = cfg.activeDBConn || ''
    agentCfg.dbSchemas = cfg.dbSchemas || {}
    agentCfg.dbSemantics = cfg.dbSemantics || {}
    agentCfg.dbLastDB = cfg.dbLastDB || {}
  } catch (e) { /* ignore */ }
}
async function saveAgentCfg() {
  try {
    await AgentAPI.saveConfig({
      activeDBConn: agentCfg.activeDBConn,
      dbSchemas: agentCfg.dbSchemas,
      dbSemantics: agentCfg.dbSemantics,
      dbLastDB: agentCfg.dbLastDB,
    })
  } catch (e) { ElMessage.error('保存失败：' + String(e)) }
}
// 输入语义时防抖落盘，避免每次按键都写磁盘
let _saveTimer = null
function debouncedSave() {
  if (_saveTimer) clearTimeout(_saveTimer)
  _saveTimer = setTimeout(() => { _saveTimer = null; saveAgentCfg() }, 300)
}
// 启用此连接作为唯一分析连接（同时仅允许一个）
async function setActiveConn(val) {
  if (val) {
    agentCfg.activeDBConn = selected.value.id
    activeConnId.value = selected.value.id
    ElMessage.success('已启用「' + selected.value.name + '」作为分析连接')
  } else if (agentCfg.activeDBConn === selected.value.id) {
    agentCfg.activeDBConn = ''
    activeConnId.value = ''
  }
  await saveAgentCfg()
}
async function onDbDatabaseChange() {
  selTables.value = []
  if (!selDatabase.value) return
  // 记住该连接上次选中的库，再次打开时自动恢复
  if (!agentCfg.dbLastDB) agentCfg.dbLastDB = {}
  agentCfg.dbLastDB[selected.value.id] = selDatabase.value
  await saveAgentCfg()
  await loadDbTables(selected.value, selDatabase.value)
}
async function syncTableSchema() {
  if (!selTables.value.length) return
  syncing.value = true
  try {
    const conn = selected.value
    for (const t of selTables.value) {
      const cols = await call(PluginDBColumns, conn, selDatabase.value, t)
      const colList = Array.isArray(cols) ? cols : (cols && cols.columns) || []
      agentCfg.dbSchemas[dbKey(conn.id, selDatabase.value, t)] = {
        connId: conn.id, database: selDatabase.value, table: t, rows: 0,
        columns: (colList || []).map(c => ({
          name: c.name, type: c.type, nullable: c.nullable, default: c.default, comment: c.comment,
        })),
      }
    }
    await saveAgentCfg()
    ElMessage.success('已同步 ' + selTables.value.length + ' 张表的结构')
    selTables.value = []
  } catch (e) {
    ElMessage.error('同步表结构失败：' + String(e))
  } finally {
    syncing.value = false
  }
}
function removeSynced(t) {
  if (!agentCfg.dbSchemas) return
  if (agentCfg.dbSemantics) {
    const p = (t.connId + '|' + t.database + '|' + t.table + '|').toLowerCase()
    Object.keys(agentCfg.dbSemantics).forEach(k => { if (k.startsWith(p)) delete agentCfg.dbSemantics[k] })
  }
  delete agentCfg.dbSchemas[dbKey(t.connId, t.database, t.table)]
  saveAgentCfg()
}

// ===================== SSH 实时终端（每标签独立会话） =====================
// 每个 SSH 标签拥有独立的 xterm 实例与会话，切换标签时保持会话存活（仅隐藏/重新挂载容器）。
async function openSsh() {
  const id = activeTabConn.value
  const conn = connOf(id)
  if (!conn || conn.category !== 'ssh') return
  if (!sshStatus[id]) sshStatus[id] = { connected: false, connecting: false, sessionId: '' }
  const st = sshStatus[id]
  const eng = sshEngines[id] || (sshEngines[id] = { term: null, fitAddon: null, ro: null, line: '' })
  if (st.connecting) return
  st.connecting = true
  try {
    await nextTick()                  // 等待 .term 容器挂载完成
    if (!termRef.value) throw new Error('终端容器未就绪')
    if (!eng.term) {
      // 首次：创建 xterm 实例（标准 VT 引擎，完整支持 ANSI/全屏 TUI）
      const term = new Terminal({
        fontSize: 13,
        fontFamily: 'Consolas, "Courier New", monospace',
        cursorBlink: true,
        scrollback: 5000,
        theme: {
          background: '#0c0c0c', foreground: '#c8e6c9', cursor: '#67c23a',
          selectionBackground: '#264f78',
        },
      })
      const fit = new FitAddon()
      term.loadAddon(fit)
      term.open(termRef.value)
      // 实时输入：每次按键直接发往远端（远端 PTY 开启 ECHO 自行回显，实现「边敲边显示」）
      term.onData((d) => onTermData(id, eng, d))
      eng.term = term
      eng.fitAddon = fit
      term.reset()
      fit.fit()
      const sid = await PluginSSHOpen(conn)
      if (!sid) throw new Error('未能建立会话')
      st.sessionId = sid
      st.connected = true
      EventsOn('ssh:' + sid + ':data', (chunk) => { const e = sshEngines[id]; if (e && e.term) e.term.write(chunk) })
      EventsOn('ssh:' + sid + ':close', () => {
        st.connected = false
        const e = sshEngines[id]
        if (e && e.term) e.term.writeln('\r\n[连接已关闭]')
      })
      nextTick(() => finalizeTerm(eng, st))
    } else if (!st.connected) {
      // 曾断开：复用已有终端实例重新建立会话
      eng.term.reset()
      eng.fitAddon.fit()
      const sid = await PluginSSHOpen(conn)
      if (!sid) throw new Error('未能建立会话')
      st.sessionId = sid
      st.connected = true
      EventsOn('ssh:' + sid + ':data', (chunk) => { const e = sshEngines[id]; if (e && e.term) e.term.write(chunk) })
      EventsOn('ssh:' + sid + ':close', () => {
        st.connected = false
        const e = sshEngines[id]
        if (e && e.term) e.term.writeln('\r\n[连接已关闭]')
      })
      nextTick(() => finalizeTerm(eng, st))
    } else {
      // 已连接：将终端容器重新挂载到当前标签（从其它标签切回时）
      await attachExisting(eng, st)
    }
  } catch (e) {
    ElMessage.error('SSH 连接失败：' + (e.message || e))
  } finally {
    st.connecting = false
  }
}

// 将已有终端实例的 DOM 重新挂到当前激活容器的尾部，并重新 fit
async function attachExisting(eng, st) {
  await nextTick()
  if (!termRef.value) return
  if (eng.term && eng.term.element && eng.term.element.parentNode !== termRef.value) {
    termRef.value.appendChild(eng.term.element)
  }
  if (eng.fitAddon) eng.fitAddon.fit()
  finalizeTerm(eng, st)
}

// 同步 PTY 尺寸、监听窗口变化并聚焦
function finalizeTerm(eng, st) {
  if (!termRef.value) return
  updateTermSize(eng, st)
  if (eng.ro) eng.ro.disconnect()
  eng.ro = new ResizeObserver(() => updateTermSize(eng, st))
  eng.ro.observe(termRef.value)
  if (eng.term) eng.term.focus()
}

// 「断开」当前 SSH 标签（保留终端实例，可再次连接恢复）
function closeSshActive() { closeSshById(activeTabConn.value) }
function closeSshById(id) {
  const st = sshStatus[id]
  const eng = sshEngines[id]
  if (eng && eng.ro) { eng.ro.disconnect(); eng.ro = null }
  if (st && st.sessionId) {
    EventsOff('ssh:' + st.sessionId + ':data')
    EventsOff('ssh:' + st.sessionId + ':close')
    PluginSSHClose(st.sessionId).catch(() => {})
    st.sessionId = ''
    st.connected = false
  }
}
// 关闭标签时彻底释放 SSH 资源（断开并销毁终端实例）
function disposeSsh(id) {
  closeSshById(id)
  const eng = sshEngines[id]
  if (eng && eng.term) { try { eng.term.dispose() } catch (e) {} }
  delete sshEngines[id]
  delete sshStatus[id]
}

function clearSshLog() { const e = sshEngines[activeTabConn.value]; if (e && e.term) e.term.reset() }
// 终端点击任意处即聚焦输入（模仿 XShell 体验）
function focusTermInput() { const e = sshEngines[activeTabConn.value]; if (e && e.term) e.term.focus() }
// 将前端终端容器尺寸同步给远端 PTY（行/列），适配 vim/top/htop 等全屏程序
function updateTermSize(eng, st) {
  if (!eng || !eng.term || !eng.fitAddon || !st || !st.connected || !st.sessionId) return
  try {
    eng.fitAddon.fit()
    PluginSSHResize(st.sessionId, eng.term.rows, eng.term.cols).catch(() => {})
  } catch (e) {}
}

// ===================== SFTP / FTP =====================
// 根据连接类别返回对应的后端 API 集合
function api() {
  return selected.value.category === 'sftp'
    ? {
        list: PluginSFTPList, read: PluginSFTPRead, write: PluginSFTPWrite, mkdir: PluginSFTPMkdir,
        del: PluginSFTPDelete, rename: PluginSFTPRename, download: PluginSFTPDownload, upload: PluginSFTPUploadB64,
      }
    : {
        list: PluginFTPList, read: PluginFTPRead, write: PluginFTPWrite, mkdir: PluginFTPMkdir,
        del: PluginFTPDelete, rename: PluginFTPRename, download: PluginFTPDownload, upload: PluginFTPUploadB64,
      }
}
// 字节数转可读大小
function fmtSize(n) {
  const v = Number(n) || 0
  if (v < 1024) return v + ' B'
  if (v < 1024 * 1024) return (v / 1024).toFixed(1) + ' KB'
  if (v < 1024 * 1024 * 1024) return (v / 1024 / 1024).toFixed(1) + ' MB'
  return (v / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}
// 拼接远端路径
function joinRemote(dir, name) {
  const d = (dir || '/').replace(/\/+$/, '')
  return (d === '' ? '' : d) + '/' + name
}
async function listRemote() {
  const r = await call(api().list, selected.value, remotePath.value)
  if (r) remoteFiles.value = r
}
async function remoteRead(row) {
  if (row.isDir) return
  currentRemotePath.value = row.path
  remoteContent.value = (await call(api().read, selected.value, row.path)) || ''
}
async function remoteWrite() {
  if (!currentRemotePath.value) return
  await call(api().write, selected.value, currentRemotePath.value, remoteContent.value)
  ElMessage.success('已写入')
}
async function remoteMkdir() {
  if (!remoteMkdirName.value) return
  const p = (remotePath.value === '/' ? '' : remotePath.value) + '/' + remoteMkdirName.value
  await call(api().mkdir, selected.value, p)
  remoteMkdirShown.value = false
  remoteMkdirName.value = ''
  await listRemote()
}
async function remoteDelete(row) {
  try {
    await ElMessageBox.confirm(`确认删除「${row.name}」？该操作不可恢复`, '删除确认', { type: 'warning' })
  } catch (e) { return }
  // FTP 删除目录需以 / 结尾，后端据此区分 DELE / RMD
  await call(api().del, selected.value, row.isDir ? row.path.replace(/\/+$/, '') + '/' : row.path)
  ElMessage.success('已删除')
  remoteChecked.value = []
  await listRemote()
}
// 批量删除选中的文件 / 目录
async function remoteDeleteBatch() {
  const rows = remoteChecked.value.slice()
  if (!rows.length) return
  try {
    await ElMessageBox.confirm(`确认删除选中的 ${rows.length} 项？该操作不可恢复`, '批量删除', { type: 'warning' })
  } catch (e) { return }
  let ok = 0
  for (const row of rows) {
    try {
      await api().del(selected.value, row.isDir ? row.path.replace(/\/+$/, '') + '/' : row.path)
      ok++
    } catch (e) { /* 单项失败继续处理其余项 */ }
  }
  ElMessage.success(`已删除 ${ok}/${rows.length} 项`)
  remoteChecked.value = []
  await listRemote()
}
// 下载：由后端弹出保存对话框并写入本地
async function remoteDownload(row) {
  if (row.isDir) { ElMessage.warning('暂不支持下载整个目录'); return }
  try {
    const local = await api().download(selected.value, row.path, row.name)
    if (local) ElMessage.success('已保存到：' + local)
  } catch (err) {
    ElMessage.error('下载失败：' + (err && err.message ? err.message : err))
  }
}
// 重命名
function openRename(row) {
  renameRow.value = row
  renameName.value = row.name
  renameShown.value = true
}
async function doRename() {
  const row = renameRow.value
  const name = (renameName.value || '').trim()
  if (!row || !name || name === row.name) { renameShown.value = false; return }
  if (name.includes('/')) { ElMessage.warning('名称不能包含 /'); return }
  try {
    await api().rename(selected.value, row.path, joinRemote(remotePath.value, name))
    renameShown.value = false
    ElMessage.success('已重命名')
    await listRemote()
  } catch (err) {
    ElMessage.error('重命名失败：' + (err && err.message ? err.message : err))
  }
}
function gotoParent() {
  const p = remotePath.value || '/'
  if (p === '/' || p === '') return
  const idx = p.lastIndexOf('/')
  remotePath.value = idx <= 0 ? '/' : p.slice(0, idx)
  listRemote()
}
function gotoSeg(seg) { remotePath.value = seg.path; listRemote() }
function onRemoteRowDblclick(row) {
  if (row.isDir) gotoSeg({ path: row.path })
  else remoteRead(row)
}

// ===================== 本地文件上传（SFTP 通道，二进制安全） =====================
// 选择本地文件并交由指定处理器上传（SFTP 面板或 SSH 终端共用）
function pickUpload(handler) {
  uploadHandler = handler
  if (fileInputRef.value) fileInputRef.value.click()
}
async function onFilePicked(e) {
  const files = Array.from(e.target.files || [])
  e.target.value = ''           // 重置，允许重复选择同一文件
  if (uploadHandler && files.length) {
    try { await uploadHandler(files) }
    catch (err) { ElMessage.error('上传失败：' + (err && err.message ? err.message : err)) }
  }
  uploadHandler = null
}
// 本地文件 -> base64（FileReader 读取，兼容任意二进制内容）
function fileToBase64(file) {
  return new Promise((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => {
      const bytes = new Uint8Array(r.result)
      let bin = ''
      const CHUNK = 0x8000
      for (let i = 0; i < bytes.length; i += CHUNK) {
        bin += String.fromCharCode.apply(null, bytes.subarray(i, i + CHUNK))
      }
      resolve(btoa(bin))
    }
    r.onerror = reject
    r.readAsArrayBuffer(file)
  })
}
// SFTP / FTP 面板：上传到当前浏览目录
async function uploadToRemote(files) {
  const up = api().upload
  for (const f of files) {
    const b64 = await fileToBase64(f)
    await call(up, selected.value, remotePath.value || '/', f.name, b64)
  }
  ElMessage.success(`已上传 ${files.length} 个文件到 ${remotePath.value || '/'}`)
  await listRemote()
}
// SSH 终端：上传到远端当前工作目录（通过 pwd 获取），等效 XShell 中 rz 的效果
async function uploadToSsh(files) {
  const conn = connOf(activeTabConn.value)
  if (!conn) return
  const pwd = (await PluginSSHExec(conn, 'pwd') || '').trim()
  const dir = pwd || '/'
  for (const f of files) {
    const b64 = await fileToBase64(f)
    await call(PluginSFTPUploadB64, conn, dir, f.name, b64)
  }
  ElMessage.success(`已上传 ${files.length} 个文件到 ${dir}`)
  const eng = sshEngines[activeTabConn.value]
  if (eng && eng.term) eng.term.writeln(`\r\n[rz 上传完成] ${files.length} 个文件 -> ${dir}`)
}

// ===================== ZMODEM 风格：rz 上传 / sz 下载 =====================
// 在 SSH 终端支持以 rz / sz 命令（或工具栏按钮）触发文件传输；
// 实际走 SFTP 安全通道完成，无需远端安装 lrzsz。
function basenameOf(p) { const s = (p || '').split('/'); return s[s.length - 1] || 'download.bin' }

// 拦截终端输入：跟踪当前行，遇到 rz / sz 命令时本地处理（取消远端待提交输入后走 SFTP）
function onTermData(id, eng, d) {
  const st = sshStatus[id]
  for (let i = 0; i < d.length; i++) {
    const ch = d[i]
    if (ch === '\r' || ch === '\n') {
      const cmd = (eng.line || '').trim()
      eng.line = ''
      if (cmd === 'rz' || /^sz(\s|$)/i.test(cmd)) {
        // 取消远端已回显但未提交的输入行，再走本地 SFTP 通道（无需远端 lrzsz）
        if (st && st.sessionId) PluginSSHInput(st.sessionId, '\x03')
        runZmodem(id, cmd)
        return
      }
      if (st && st.sessionId) PluginSSHInput(st.sessionId, ch)
    } else if (ch === '\x7f' || ch === '\b') {
      if (eng.line) eng.line = eng.line.slice(0, -1)
      if (st && st.sessionId) PluginSSHInput(st.sessionId, ch)
    } else {
      eng.line = (eng.line || '') + ch
      if (st && st.sessionId) PluginSSHInput(st.sessionId, ch)
    }
  }
}

async function runZmodem(id, cmd) {
  const conn = connOf(id)
  if (!conn) return
  if (cmd === 'rz') {
    // 上传：复用 SFTP 通道写入远端当前工作目录（等效 rz）
    pickUpload(uploadToSsh)
    return
  }
  const path = cmd.slice(2).trim()
  if (!path) { ElMessage.warning('sz 用法：sz <远端文件路径>'); return }
  await runSzByPath(id, path)
}

async function runSzByPath(id, path) {
  const conn = connOf(id)
  if (!conn) return
  try {
    const local = await call(PluginSFTPDownload, conn, path, basenameOf(path))
    const eng = sshEngines[id]
    if (eng && eng.term) eng.term.writeln(`\r\n[sz 下载完成] ${path} -> ${local || '(已取消)'}`)
    if (!local) ElMessage.info('已取消下载')
    else ElMessage.success(`已下载：${local}`)
  } catch (e) {
    ElMessage.error('sz 下载失败：' + (e && e.message ? e.message : e))
  }
}

// sz 下载对话框（工具栏按钮）：输入远端文件路径后保存到本地
const szDialogVisible = ref(false)
const szRemotePath = ref('')
function openSzDialog() { szRemotePath.value = ''; szDialogVisible.value = true }
async function confirmSz() {
  const path = szRemotePath.value.trim()
  if (!path) { ElMessage.warning('请输入远端文件路径'); return }
  szDialogVisible.value = false
  await runSzByPath(activeTabConn.value, path)
}

function removeConn(id) {
  removePluginConn(id)
  if (openTabs.value.includes(id)) closeConnTab(id)
  ElMessage.success('已删除')
}
</script>

<style scoped>
.pm { display: flex; flex-direction: column; height: 100%; }

/* 常驻标签栏（类浏览器标签，无标签时也显示） */
.pm-tabs {
  display: flex; align-items: stretch; gap: 2px; padding: 6px 12px 0;
  background: var(--surface, #fff); border-bottom: 1px solid #e5e6eb; flex-shrink: 0;
  overflow-x: auto; min-height: 38px;
}
.pm-tabs-empty { font-size: 12px; color: #86909c; align-self: center; padding: 0 4px; }
.pm-tab {
  display: flex; align-items: center; gap: 6px; max-width: 200px; padding: 6px 8px;
  font-size: 13px; color: #4e5969; cursor: pointer; white-space: nowrap;
  border: 1px solid transparent; border-bottom: none; border-radius: 8px 8px 0 0;
  background: #f2f3f5; user-select: none;
}
.pm-tab:hover { color: #1d2129; background: #e8eaed; }
.pm-tab.active {
  color: #1d2129; background: #fff; border-color: #e5e6eb; font-weight: 600;
  position: relative; top: 1px;
}
.pm-tab-ico { font-size: 13px; line-height: 1; }
.pm-tab-name { overflow: hidden; text-overflow: ellipsis; }
.pm-tab-close {
  width: 16px; height: 16px; line-height: 14px; text-align: center; border-radius: 4px;
  font-size: 14px; color: #86909c; flex-shrink: 0;
}
.pm-tab-close:hover { background: #f53f3f; color: #fff; }
.pm-tab-plus {
  align-self: center; margin-left: 4px; width: 26px; height: 26px; flex-shrink: 0;
  border: 1px dashed #c9cdd4; border-radius: 6px; background: transparent; cursor: pointer;
  font-size: 16px; color: #4e5969; line-height: 1;
}
.pm-tab-plus:hover { border-color: #165dff; color: #165dff; background: #f0f5ff; }

/* 会话选择器 */
.sel-groups { max-height: 56vh; overflow: auto; padding-right: 4px; }
.sel-group { margin-bottom: 14px; }
.sel-group-title { font-size: 12px; font-weight: 600; color: #86909c; margin-bottom: 6px; letter-spacing: .5px; }
.sel-empty { font-size: 12px; color: #c0c4cc; padding: 4px 2px; }
.sel-items { display: flex; flex-direction: column; gap: 6px; }
.sel-item {
  display: flex; align-items: center; gap: 10px; padding: 9px 12px; border-radius: 8px;
  border: 1px solid #e5e6eb; background: #fff; cursor: pointer; transition: all .15s;
}
.sel-item:hover { border-color: #165dff; background: #f0f5ff; }
.sel-ico { font-size: 15px; }
.sel-name { font-weight: 600; font-size: 13px; color: #1d2129; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sel-host { font-size: 12px; color: #86909c; font-family: Consolas, monospace; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sel-open { font-size: 12px; color: #165dff; flex-shrink: 0; }
.sel-footer { display: flex; justify-content: flex-end; }
.pm-grid-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 18px; border-bottom: 1px solid #e5e6eb; background: #fff; flex-shrink: 0;
}
.pm-grid-title { font-size: 15px; font-weight: 600; color: #1d2129; }

.pm-grid { flex: 1; overflow: auto; padding: 18px; }
.pm-cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 14px; }
.pm-card {
  border: 1px solid #e5e6eb; border-radius: 10px; padding: 14px;
  background: #fff; cursor: pointer; transition: box-shadow .15s, border-color .15s, transform .15s;
  position: relative;
}
.pm-card:hover { border-color: #165dff; box-shadow: 0 4px 14px rgba(22, 93, 255, .12); transform: translateY(-2px); }
.pm-card-top { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.pm-card-ico { font-size: 18px; }
.pm-card-name { font-weight: 600; font-size: 15px; color: #1d2129; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pm-card-host { color: #86909c; font-size: 13px; font-family: Consolas, monospace; }
.pm-card-meta { color: #a9aeb8; font-size: 12px; margin-top: 4px; min-height: 18px; }
.pm-card-actions { display: flex; gap: 14px; margin-top: 12px; border-top: 1px dashed #eee; padding-top: 10px; }
.pm-act { font-size: 13px; color: #165dff; cursor: pointer; }
.pm-act:hover { text-decoration: underline; }
.pm-act-del { color: #f53f3f; }

.pm-detail { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow: hidden; padding: 14px 18px; }
.pm-detail-bar { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; }
.pm-detail-title { font-size: 16px; font-weight: 600; color: #1d2129; }
.pm-detail-tag { font-size: 12px; color: #165dff; background: #e8f3ff; border-radius: 4px; padding: 1px 8px; }
.pm-detail-host { font-size: 13px; color: #86909c; font-family: Consolas, monospace; }
.pm-pre { white-space: pre-wrap; word-break: break-all; background: #f7f7f7; padding: 8px; border-radius: 4px; margin: 0; }
.pm-path { font-size: 13px; color: #409eff; }
.pm-path-seg { cursor: pointer; }
.pm-path-seg:hover { text-decoration: underline; }

/* 模仿 XShell 的实时终端 */
.term-bar { display: flex; align-items: center; gap: 10px; padding: 6px 10px; background: #1e1e1e; border-radius: 6px 6px 0 0; flex-shrink: 0; }
.term-status { display: flex; align-items: center; gap: 6px; font-size: 12px; color: #f56c6c; }
.term-status.on { color: #67c23a; }
.term-status .dot { width: 8px; height: 8px; border-radius: 50%; background: currentColor; }
.term-host { font-size: 12px; color: #c0c4cc; font-family: Consolas, monospace; }

/* xterm.js 容器：由 FitAddon 按此容器尺寸计算行列 */
.term {
  flex: 1; min-height: 0; position: relative; overflow: hidden;
  background: #0c0c0c; border-radius: 0 0 6px 6px; cursor: text;
}
.term :deep(.xterm) {
  padding: 8px 10px; height: 100%; box-sizing: border-box;
}
.term :deep(.xterm-viewport) { background-color: transparent !important; }
.term-overlay {
  position: absolute; inset: 0; z-index: 5; display: flex; flex-direction: column; gap: 10px;
  align-items: center; justify-content: center; background: rgba(0,0,0,.55);
}
.term-hint { font-size: 12px; color: #8a8a8a; }

/* 详情操作区铺满 */
.pm-body { flex: 1; min-height: 0; overflow: auto; display: flex; flex-direction: column; }
.pm-line { display: flex; align-items: center; gap: 6px; flex-shrink: 0; margin-bottom: 8px; }
.pm-line.pm-wrap { flex-wrap: wrap; }
.pm-inline { width: 240px; flex-shrink: 0; }
.pm-sub { font-weight: 600; margin: 10px 0 4px; flex-shrink: 0; }
.pm-sep { margin-top: 8px; flex-shrink: 0; }
.pm-tbl-grow { flex: 1; min-height: 200px; margin-top: 4px; }
.pm-ssh-out { flex: 1; min-height: 0; overflow: auto; margin-top: 10px; }
.pm-content { flex-shrink: 0; }
.pm-content :deep(.el-textarea__inner) { height: 180px; resize: none; }

/* 连接卡片：激活（分析中）徽标 */
.pm-card-active { border-color: #67c23a; box-shadow: 0 0 0 2px rgba(103,194,58,.15); }
.pm-card-badge { margin-left: 6px; font-size: 11px; padding: 1px 7px; border-radius: 999px; background: #e8f5e9; color: #2e7d32; }
/* 详情：当前分析连接横幅 */
.pm-active-banner { margin: 8px 0; padding: 8px 12px; border-radius: 8px; background: #e8f5e9; color: #2e7d32; font-size: 12px; font-weight: 600; }

/* 数据库连接管理（同步表结构 / 字段语义维护）—— 后台系统风格 */
.db-mgr-card { margin-top: 10px; border-radius: 8px; }
.db-mgr-head { display: flex; align-items: center; justify-content: space-between; }
.db-mgr-title { font-weight: 600; font-size: 14px; }
.db-mgr-tools { display: flex; align-items: center; gap: 8px; }
.db-autosave { font-size: 12px; color: #67c23a; background: #f0f9eb; border: 1px solid #e1f3d8; border-radius: 4px; padding: 1px 8px; white-space: nowrap; }
.sem-edit { display: flex; align-items: center; gap: 6px; }
.sem-edit .el-input { flex: 1; }
.db-mgr-body { display: grid; grid-template-columns: 280px 1fr; gap: 16px; }
.db-pick { border: 1px solid #ebeef5; border-radius: 8px; padding: 10px; background: #fafbfc; }
.db-pick-bar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; font-weight: 600; font-size: 13px; color: #1d2129; }
.db-synced { min-width: 0; }
.table-pick { max-height: 460px; overflow: auto; }
.tp-list { display: flex; flex-direction: column; gap: 6px; }
.tp-list :deep(.el-checkbox) { margin-right: 0; width: 100%; }
.tp-synced { color: #2e7d32; font-size: 11px; margin-left: 4px; }
.synced-table { background: #fff; }
.col-table { margin: 8px 12px 12px; }
.table-sem-inline { display: flex; align-items: center; gap: 10px; padding: 8px 12px 4px; }
.sem-col.sm { min-width: 52px; color: #888; font-size: 12px; }
.muted { color: #c0c4cc; }
.pm-hint.pm-block { margin: 4px 0; }
.pm-test { font-size: 12px; padding: 2px 8px; border-radius: 4px; }
.pm-test.ok { color: #2e7d32; background: #e8f5e9; }
.pm-test.err { color: #c62828; background: #ffebee; }
.empty-hint { color: #a9aeb8; font-size: 12px; padding: 10px 0; }
@media (max-width: 860px) { .db-mgr-body { grid-template-columns: 1fr; } }
</style>
