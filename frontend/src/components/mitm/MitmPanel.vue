<template>
  <div class="mitm-panel">
    <!-- 顶部控制栏 -->
    <div class="mitm-top">
      <div class="mitm-title">
        <h2>网络抓包 (MITM)</h2>
        <el-popover placement="bottom-start" :width="400" trigger="click" popper-class="intro-pop">
          <template #reference>
            <span class="sub-more" title="查看功能介绍">功能介绍 ▾</span>
          </template>
          <div class="intro-box">
            <div class="intro-line"><b>轻量级流量解密</b>：内置 MITM 代理，解密 HTTP/HTTPS，并识别 WebSocket / SSE / gRPC / GraphQL。</div>
            <div class="intro-line"><b>API 文档自动生成</b>：抓到的请求支持批量或一键生成接口文档，直接导入接口树。</div>
            <div class="intro-tip">提示：未安装根证书时仅能抓取明文 HTTP，HTTPS 会被降级透传（不解密）。</div>
          </div>
        </el-popover>
      </div>
      <div class="mitm-actions">
        <template v-if="!status.running">
          <el-input v-model="proxyAddr" size="small" style="width: 190px"
            placeholder="监听地址 如 127.0.0.1:8888" title="混合代理监听地址，同一端口同时支持 HTTP/HTTPS 代理与 SOCKS5 代理；0 表示随机端口" />
          <el-switch v-model="sysProxy" size="small" active-text="切换系统代理" @change="setSysProxy" />
          <el-button type="success" @click="startSniff" :loading="starting">开始抓包</el-button>
        </template>
        <template v-else>
          <el-tag type="success" effect="dark" class="addr-tag" title="点击展开/收起手机证书下载信息"
            @click="toggleCert">抓包中 · {{ status.proxyAddr }}（点击看手机证书）</el-tag>
          <el-tag v-if="status.systemProxy" type="info">系统代理已开</el-tag>
          <el-tag v-else type="warning">仅监听端口</el-tag>
          <el-button type="primary" plain size="small" @click="copyProxyAddr" title="复制代理地址到剪贴板">复制代理地址</el-button>
          <el-button type="danger" @click="stopSniff" :loading="stopping">停止并保存</el-button>
        </template>
        <el-button @click="installCA" :disabled="status.caInstalled" :loading="installing">
          {{ status.caInstalled ? 'CA 已安装' : '安装根证书' }}
        </el-button>
        <el-button @click="openCADialog">查看证书</el-button>
        <el-button @click="openImportCADialog">导入证书</el-button>
        <el-button @click="openSessions">抓包会话</el-button>
        <el-button @click="openErrors">解密失败日志<span v-if="errCount > 0" class="err-badge">{{ errCount }}</span></el-button>
        <el-button type="warning" plain @click="openRewrites" title="把请求的域名改写为目标地址，并支持 Query 参数/请求头的自由替换（如 dev.test.com → 127.0.0.1:8200）">
          请求改写
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="!status.caInstalled && !status.running"
      type="warning"
      :closable="false"
      show-icon
      title="HTTPS 解密需安装根证书"
      description="点击右上角「安装根证书」将 ApiTool 自签 CA 加入系统信任库（需以管理员身份运行）。未安装时仅能抓取明文 HTTP，HTTPS 会被降级透传（不解密）。" />

    <!-- 手机抓包：默认隐藏，点击控制栏「抓包中」地址才展开（不挤压下方流量列表） -->
    <el-collapse-transition>
      <el-card v-if="certOpen && status.running && status.certURL" class="cert-card" shadow="never">
        <template #header>
          <div class="cert-head">
            <span>📱 手机抓包证书（下载根证书用于手机 HTTPS 抓包）</span>
            <el-tag size="small" type="success" effect="plain">运行中</el-tag>
          </div>
        </template>
        <div class="cert-body">
          <div class="cert-row">
            <span class="cert-label">手机下载</span>
            <code class="cert-url">{{ status.certURL }}</code>
            <el-button size="small" text type="primary" @click="copyText(status.certURL)">复制</el-button>
          </div>
          <div class="cert-row">
            <span class="cert-label">本机下载</span>
            <code class="cert-url">{{ status.localCertURL }}</code>
            <el-button size="small" text type="primary" @click="copyText(status.localCertURL)">复制</el-button>
          </div>
          <ol class="cert-steps">
            <li>手机与电脑连同一 Wi-Fi，WLAN 里把 HTTP 代理手动指向电脑 IP 与端口。</li>
            <li>手机浏览器打开上方「手机下载」地址，点击下载并安装根证书。</li>
            <li>iOS：安装后到「设置 → 通用 → 关于本机 → 证书信任设置」启用完全信任。</li>
            <li>返回本工具即可实时查看手机 HTTPS 流量。</li>
          </ol>
        </div>
      </el-card>
    </el-collapse-transition>

    <el-alert
      v-if="status.error"
      type="error"
      :closable="false"
      show-icon
      :title="status.error" />

    <!-- 连接/证书错误单独展示（可被「隐藏错误」开关屏蔽） -->
    <div v-if="showErrors && errorList.length" class="mitm-errors">
      <el-alert v-for="(e, i) in errorList.slice(0, 3)" :key="i" type="error" :closable="false"
        :show-icon="false" class="err-item">
        <div class="err-head">
          <span class="err-time">{{ e.time }}</span>
          <el-tag size="small" :type="errTagType(e.type)" class="err-tag">{{ errLabel(e.type) }}</el-tag>
          <span v-if="e.host" class="err-host" @click="setLiveFilter(e.host)">@ {{ e.host }}</span>
          <span style="flex:1"></span>
          <el-button link size="small" type="primary" @click="copyText(e.msg)">复制</el-button>
          <el-button link size="small" @click="errorList.splice(i, 1)">忽略</el-button>
        </div>
        <div class="err-body">{{ e.msg }}</div>
      </el-alert>
      <div class="err-foot">
        <span>共 {{ errorList.length }} 条错误</span>
        <el-button link size="small" @click="errorList = []">清空</el-button>
      </div>
    </div>

    <!-- 解决引导（可被「隐藏错误」开关屏蔽） -->
    <div v-if="showErrors && currentGuide" class="mitm-guide" :class="'g-' + currentGuide.type">
      <div class="g-head">
        <span class="g-title">解决建议：{{ errLabel(currentGuide.type) }}</span>
        <el-button link size="small" type="primary" @click="copyText(currentGuide.solution)">复制方案</el-button>
      </div>
      <div class="g-body">{{ currentGuide.solution }}</div>
      <div class="g-act" v-if="currentGuide.action">
        <el-button size="small" @click="currentGuide.action.fn()">{{ currentGuide.action.label }}</el-button>
      </div>
    </div>

    <div class="mitm-body">
      <!-- 左：流量列表 + 过滤 -->
      <div class="mitm-left">
        <!-- 过滤条件（默认收起，点击标题栏展开，收起时显示当前条件摘要） -->
        <div class="filter-box" :class="{ 'is-collapsed': !filterOpen }">
          <div class="filter-head" @click="filterOpen = !filterOpen">
            <span class="fh-caret">{{ filterOpen ? '▾' : '▸' }}</span>
            <span class="fh-title">过滤条件</span>
            <el-tag v-if="filterCount > 0" size="small" type="primary" effect="plain" class="fh-badge">{{ filterCount }}</el-tag>
            <span v-if="!filterOpen" class="fh-summary" :title="filterSummary">{{ filterSummary }}</span>
            <span v-if="!filterOpen" class="fh-hint">展开</span>
          </div>
          <el-collapse-transition>
            <div class="filter-body" v-show="filterOpen">
              <div class="filter-row">
                <span class="fl">Host 过滤（逗号分隔，留空=全部）</span>
                <el-input v-model="filterHosts" size="small" placeholder="example.com, api.test.cn" @change="applyFilter" />
              </div>
              <div class="filter-row">
                <span class="fl">排除 Host</span>
                <el-input v-model="filterExclude" size="small" placeholder="localhost, 127.0.0.1" @change="applyFilter" />
              </div>
              <div class="filter-row">
                <span class="fl">协议勾选</span>
                <el-checkbox-group v-model="filterProtocols" size="small" @change="applyFilter">
                  <el-checkbox-button value="http">HTTP</el-checkbox-button>
                  <el-checkbox-button value="https">HTTPS</el-checkbox-button>
                  <el-checkbox-button value="websocket">WebSocket</el-checkbox-button>
                  <el-checkbox-button value="sse">SSE</el-checkbox-button>
                  <el-checkbox-button value="grpc">gRPC</el-checkbox-button>
                  <el-checkbox-button value="graphql">GraphQL</el-checkbox-button>
                </el-checkbox-group>
                <el-tooltip content="勾选哪些协议就解析哪些；一个都不选则全部抓取解析" placement="top">
                  <span style="color:#86909c;margin-left:6px;cursor:help;font-size:13px">?</span>
                </el-tooltip>
              </div>
              <div class="filter-row">
                <el-checkbox v-model="filterOnlyHTTP" @change="applyFilter">仅抓取 HTTP/HTTPS</el-checkbox>
                <el-checkbox v-model="autoDoc" border size="small" title="抓取时自动为每个请求生成文档草稿">自动生成文档</el-checkbox>
                <el-switch v-model="showErrors" size="small" inline-prompt active-text="显示错误" inactive-text="隐藏错误"
                  style="--el-switch-on-color:#f56c6c;margin-left:8px" title="关闭后隐藏解密失败/连接错误的提示列表、解决引导与弹窗提示（错误仍记录可在「解密失败日志」查看）" />
              </div>
            </div>
          </el-collapse-transition>
        </div>

        <!-- 实时流量（标题栏可折叠收起，避免大列表占用主界面空间） -->
        <div class="traffic-head" @click="trafficOpen = !trafficOpen">
          <span class="th-caret">{{ trafficOpen ? '▾' : '▸' }}</span>
          <span class="th-title">实时流量（{{ filteredRecords.length }} / {{ liveRecords.length }}）
            <em v-if="filteredRecords.length > MAX_RENDER" class="render-limit">仅显示最新 {{ MAX_RENDER }} 条</em>
          </span>
          <div class="th-actions" @click.stop>
            <el-input v-model="liveFilter" size="small" placeholder="搜索 host/url/方法" clearable style="width:120px" />
            <el-radio-group v-model="viewMode" size="small">
              <el-radio-button value="list">平铺</el-radio-button>
              <el-radio-button value="group">分组</el-radio-button>
            </el-radio-group>
            <el-switch v-model="onlyErr" size="small" active-text="仅异常" style="--el-switch-on-color:#f56c6c"
              title="仅显示有解密失败记录的连接" @change="onlyErr = !!onlyErr" />
            <el-select v-if="onlyErr" v-model="errTypeFilter" size="small" placeholder="错误类型" style="width:130px" clearable>
              <el-option v-for="t in errTypeOptions" :key="t.value" :label="t.label" :value="t.value" />
            </el-select>
            <el-button link size="small" @click="selectAll">全选</el-button>
            <template v-if="selectedIds.length">
              <el-button link type="primary" size="small" @click="openBatchImport">批量导入（{{ selectedIds.length }}）</el-button>
              <el-button link size="small" @click="selectedIds = []">取消选择</el-button>
            </template>
            <el-button link type="primary" size="small" @click="clearLive">清空</el-button>
          </div>
        </div>
        <el-collapse-transition>
          <div class="traffic-list" v-show="trafficOpen">
            <div v-if="!filteredRecords.length" class="empty">
              <div class="empty-ico">🛰️</div>
              {{ liveRecords.length ? '没有匹配的流量（可调整过滤条件）' : '暂无流量，开始抓包后系统流量将实时显示在这里' }}
            </div>

            <!-- 平铺模式 -->
            <template v-if="viewMode === 'list'">
              <div
                v-for="r in listRecords"
                :key="r.id"
                class="traffic-item"
                :class="{ active: selected && selected.id === r.id, checked: selectedIdSet.has(r.id) }"
                @click="selectRecord(r)"
                @contextmenu.prevent="openCtxMenu(r, $event)">
                <el-checkbox size="small" :model-value="selectedIdSet.has(r.id)" @click.stop
                  @change="(v) => toggleSelect(r.id, v)" />
                <span class="proto" :class="'p-' + r.protocol.toLowerCase()">{{ r.protocol }}</span>
                <span class="method" v-if="r.method" :class="'m-' + r.method.toUpperCase()">{{ r.method }}</span>
                <span class="url" :title="r.url">{{ r.url || r.host }}</span>
                <span class="status" v-if="r.statusCode" :class="'s-' + String(r.statusCode)[0]">{{ r.statusCode }}</span>
              </div>
            </template>

            <!-- 分组模式：Charles 风格分层树（host → 目录 → 请求），默认折叠，可逐个展开 -->
            <template v-else>
              <div v-if="!groupedRecords.length" class="empty"><div class="empty-ico">🛰️</div>暂无流量</div>
              <GroupTreeNode
                v-for="g in groupedRecords"
                :key="g.key"
                :node="g"
                :expanded="expandedKeys.has(g.key)"
                :expanded-set="expandedKeys"
                :selected="selected"
                :selected-set="selectedIdSet"
                @toggle="toggleGroup"
                @select-node="onGroupSelectNode"
                @select-rec="selectRecord"
                @select-rec-toggle="onSelectRecToggle"
                @ctx-menu="openCtxMenu"
              />
            </template>
          </div>
        </el-collapse-transition>
      </div>

      <!-- 右：详情 -->
      <div class="mitm-right">
        <template v-if="selected">
          <div class="detail-head">
            <div class="dh-meta">
              <el-tag size="small" type="primary">{{ selected.method || selected.protocol }}</el-tag>
              <span class="du" :title="selected.url">{{ selected.url || selected.host }}</span>
            </div>
            <div class="dh-actions">
              <el-button size="small" @click="copyText(selected.reqBody)">复制请求体</el-button>
              <el-button size="small" type="primary" @click="openImportDialog">生成接口并导入接口树</el-button>
            </div>
          </div>
          <el-tabs v-model="detailTab" class="detail-tabs">
            <el-tab-pane label="概览" name="overview">
              <div class="kv"><b>协议</b><span>{{ selected.protocol }} <i v-if="!selected.decrypted">（未解密）</i></span></div>
              <div class="kv"><b>状态</b><span>{{ selected.statusCode || '—' }} {{ selected.statusText }}</span></div>
              <div class="kv"><b>耗时</b><span>{{ selected.durationMs }} ms</span></div>
              <div class="kv"><b>Host</b><span>{{ selected.host }}</span></div>
              <div class="kv"><b>说明</b><span>{{ selected.note || '—' }}</span></div>
              <div class="kv"><b>抓包时间</b><span>{{ selected.timestamp ? formatTime(selected.timestamp) : '—' }}</span></div>
            </el-tab-pane>
            <el-tab-pane label="请求头" name="reqh">
              <div class="body-toolbar">
                <el-button link type="primary" size="small" @click="copyText(kvToText(selected.reqHeaders))">复制</el-button>
              </div>
              <pre class="code">{{ kvToText(selected.reqHeaders) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="请求体" name="reqb">
              <div class="body-toolbar">
                <el-button link type="primary" size="small" @click="copyText(displayBody(selected.reqBody, true))">复制</el-button>
                <el-button link type="primary" size="small" @click="toggleFormat('req')">
                  {{ reqFormatted ? '查看原文' : '格式化' }}
                </el-button>
              </div>
              <pre class="code">{{ displayBody(selected.reqBody, reqFormatted) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="原始请求" name="rawreq">
              <div class="body-toolbar">
                <el-button link type="primary" size="small" @click="copyText(buildRawHttp(selected))">复制</el-button>
              </div>
              <pre class="code">{{ buildRawHttp(selected) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="响应头" name="resh">
              <div class="body-toolbar">
                <el-button link type="primary" size="small" @click="copyText(kvToText(selected.respHeaders))">复制</el-button>
              </div>
              <pre class="code">{{ kvToText(selected.respHeaders) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="响应体" name="resb">
              <div v-if="selected.respClipped" class="clipped-tip">⚠️ 响应体超过实时推送上限（1MB）已被截断，会话历史中保留完整数据</div>
              <template v-if="isImageResp(selected)">
                <div class="img-preview">
                  <img :src="imgSrc(selected.respBody)" alt="响应图片预览" />
                </div>
              </template>
              <template v-else>
                <div class="body-toolbar">
                  <el-button link type="primary" size="small" @click="copyText(displayBody(selected.respBody, true))">复制</el-button>
                  <el-button link type="primary" size="small" @click="toggleFormat('resp')">
                    {{ respFormatted ? '查看原文' : '格式化' }}
                  </el-button>
                </div>
                <pre class="code">{{ displayBody(selected.respBody, respFormatted) }}</pre>
              </template>
            </el-tab-pane>
          </el-tabs>
        </template>
        <div v-else class="detail-empty">点击左侧流量查看详情</div>
      </div>
    </div>

    <!-- 右键上下文菜单 -->
    <div v-if="ctxMenu" class="ctx-mask" @click="closeCtxMenu" @contextmenu.prevent="closeCtxMenu">
      <div class="ctx-menu" :style="{ left: ctxMenu.x + 'px', top: ctxMenu.y + 'px' }" @click.stop @contextmenu.prevent.stop>
        <div class="ctx-item" @click="ctxCopyAddr">复制地址</div>
        <div class="ctx-item" @click="ctxReplay">重放请求</div>
        <div class="ctx-item" @click="ctxCopyCurl">复制为 curl 命令</div>
        <div class="ctx-item" @click="ctxCopyRawHttp">复制原始请求</div>
        <div class="ctx-item" @click="ctxCopyReqHeaders">复制请求头</div>
        <div class="ctx-item" @click="ctxCopyReqBody">复制请求体</div>
        <div class="ctx-item" @click="ctxCopyResBody" v-if="ctxMenu.rec.respBody">复制响应体</div>
        <div class="ctx-sep" v-if="ctxMenu.rec.respBody"></div>
        <div class="ctx-item" @click="selectRecord(ctxMenu.rec); closeCtxMenu()">查看详情</div>
      </div>
    </div>

    <!-- 导入接口树弹窗 -->
    <el-dialog v-model="importDialog" title="生成接口并导入接口树" width="480px">
      <p style="color:#86909c; font-size:13px; margin:0 0 12px">
        将当前选中的流量记录「{{ selected ? (selected.method + ' ' + (selected.path || selected.host)) : '' }}」转换为接口定义，写入所选项目/目录。
      </p>
      <div class="import-row">
        <span class="ir-label">目标项目</span>
        <el-select v-model="importProjectId" size="small" style="width: 200px">
          <el-option v-for="p in store.data.projects" :key="p.id" :label="p.name" :value="p.id" />
        </el-select>
      </div>
      <div class="import-row">
        <span class="ir-label">目标目录</span>
        <el-tree-select v-model="importDirId" :data="projectDirOptions" check-strictly
          :render-after-expand="false" size="small" style="width: 200px"
          placeholder="选择目录（默认根目录）" default-expand-all />
      </div>
      <template #footer>
        <el-button size="small" @click="importDialog = false">取消</el-button>
        <el-button size="small" type="primary" :loading="importing" @click="doImportApi">生成并导入</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="caDialog" title="根证书 (CA) 信息" width="640px">
      <p>将以下根证书安装到系统「受信任的根证书颁发机构」即可解密 HTTPS。也可点击「安装根证书」由程序自动安装（需管理员）。</p>
      <div class="kv"><b>指纹(SHA1)</b><span>{{ status.caFingerprint }}</span></div>
      <pre class="code ca">{{ caPem }}</pre>
    </el-dialog>

    <!-- 导入根证书（复用 Fiddler 等现有 CA） -->
    <el-dialog v-model="importCADialog" title="导入根证书（复用 Fiddler 等现有 CA）" width="600px">
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:10px">
        <el-button size="small" type="primary" plain @click="pickCAFile" :loading="pickingFile">选择证书文件（FiddlerRoot.cer）</el-button>
        <span style="color:#86909c;font-size:12px">或手动粘贴下方 PEM</span>
      </div>
      <p style="color:#86909c;font-size:13px;margin:0 0 10px">
        导入后将替换当前 CA。解密 HTTPS 需<b>私钥</b>；<code>FiddlerRoot.cer</code> 通常只含证书、不含私钥，若缺少私钥请从 Fiddler 导出含私钥的证书，或手动填写私钥 PEM。导入后重新「安装根证书」即可生效。
      </p>
      <div class="import-row" style="align-items:flex-start">
        <span class="ir-label">证书 PEM</span>
        <el-input v-model="importCertPem" type="textarea" :rows="5" placeholder="-----BEGIN CERTIFICATE-----&#10;..." style="flex:1" />
      </div>
      <div class="import-row" style="align-items:flex-start">
        <span class="ir-label">私钥 PEM</span>
        <el-input v-model="importKeyPem" type="textarea" :rows="5" placeholder="-----BEGIN RSA PRIVATE KEY----- / -----BEGIN PRIVATE KEY-----" style="flex:1" />
      </div>
      <template #footer>
        <el-button size="small" @click="importCADialog = false">取消</el-button>
        <el-button size="small" type="primary" :loading="importingCA" @click="doImportCA">导入并应用</el-button>
      </template>
    </el-dialog>

    <!-- 抓包会话管理（从顶部「抓包会话」按钮展开，不占用主界面） -->
    <el-dialog v-model="sessionsDialog" title="抓包会话" width="720px" @open="refreshSessions">
      <div class="sess-toolbar">
        <span class="sess-count">已保存 {{ sessions.length }} 个会话</span>
        <div>
          <el-button size="small" type="primary" plain @click="refreshSessions">刷新</el-button>
          <el-button size="small" type="danger" plain :disabled="!sessions.length"
            @click="clearAllSessions" title="一键清除全部会话包">一键清除会话包</el-button>
        </div>
      </div>
      <el-table :data="sessions" size="small" empty-text="暂无会话" max-height="360">
        <el-table-column prop="name" label="会话名" min-width="200" />
        <el-table-column prop="startedAt" label="开始时间" width="180" />
        <el-table-column label="记录数" width="90">
          <template #default="{ row }">{{ row.recordCount ?? (row.records || []).length }}</template>
        </el-table-column>
        <el-table-column label="操作" width="240">
          <template #default="{ row }">
            <el-button link type="primary" @click="exportSession(row.id, row.name)">导出 OpenAPI</el-button>
            <el-button link type="danger" @click="deleteSession(row.id)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <!-- 解密失败日志 -->
    <el-dialog v-model="errorDialog" title="解密 / 连接失败日志" width="640px" append-to-body>
      <div v-if="errList.length === 0" class="empty-box">
        <div class="empty-ico">✅</div>
        <div>暂无失败记录，所有流量均已正常解密。</div>
      </div>
      <div v-else class="err-list">
        <div v-for="(e, i) in errList" :key="i" class="err-item">
          <div class="err-row">
            <span class="err-type" :class="'et-' + e.type">{{ errTypeText(e.type) }}</span>
            <span v-if="e.host" class="err-host">{{ e.host }}</span>
          </div>
          <pre class="err-msg">{{ e.message }}</pre>
        </div>
      </div>
    </el-dialog>

    <!-- 请求改写（域名重定向 + 参数替换） -->
    <el-dialog v-model="rewritesDialog" title="请求改写（域名重定向 + 参数替换）" width="760px" append-to-body>
      <p class="rw-tip">
        将请求的域名改写为目标地址，便于测试。例如
        <code>dev.test.com → 127.0.0.1:8200</code>（未写端口时 HTTPS 自动补 :443、HTTP 补 :80；
        目标端口非 443 时默认按 HTTP 转发，本地联调服务通常为明文 HTTP）。
        <b>To</b> 也支持直接填域名，并可带路径与查询串，如
        <code>api.test.com/v2/api?v=2</code>（命中后整体替换路径与参数）。
        点击「参数」可对该请求做 <b>Query 参数 / 请求头</b> 的自由替换、新增与删除。
        配置保存后立即对后续抓到的请求生效，HTTPS 仍按原域名签发证书。
      </p>
      <el-table :data="rewrites" size="small" empty-text="暂无配置，点击下方「添加一行」" max-height="360">
        <el-table-column label="启用" width="64" align="center">
          <template #default="{ row }">
            <el-switch v-model="row.enabled" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="原域名 (From)" min-width="170">
          <template #default="{ row }">
            <el-input v-model="row.from" size="small" placeholder="dev.test.com" clearable />
          </template>
        </el-table-column>
        <el-table-column label="改写为 (To)" min-width="190">
          <template #default="{ row }">
            <el-input v-model="row.to" size="small" placeholder="127.0.0.1:8200 或 api.test.com/v2/api?v=2" clearable />
          </template>
        </el-table-column>
        <el-table-column label="协议" width="100" align="center">
          <template #default="{ row }">
            <el-select v-model="row.scheme" size="small" style="width: 88px" placeholder="自动">
              <el-option label="自动" value="auto" />
              <el-option label="HTTP" value="http" />
              <el-option label="HTTPS" value="https" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="备注" min-width="100">
          <template #default="{ row }">
            <el-input v-model="row.desc" size="small" placeholder="可选" clearable />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="132" align="center">
          <template #default="{ row, $index }">
            <el-button link type="primary" size="small" @click="openRepl(row)">{{ replLabel(row) }}</el-button>
            <el-button link type="danger" size="small" @click="removeRewrite($index)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button size="small" @click="rewritesDialog = false">取消</el-button>
        <el-button size="small" @click="addRewrite">添加一行</el-button>
        <el-button size="small" type="primary" :loading="savingRewrites" @click="saveRewrites">保存</el-button>
      </template>
    </el-dialog>

    <!-- 参数替换（Query 参数 / 请求头的替换、新增、删除） -->
    <el-dialog v-model="replDialog" :title="`参数替换 — ${replTitle}`" width="760px" append-to-body>
      <p class="rw-tip">
        对命中该改写规则的请求替换参数：<b>Query 参数</b>作用于 URL 查询串（<code>?a=1</code>），
        <b>请求头</b>作用于 HTTP Header（如 <code>Authorization</code>、<code>Host</code>）。
        <b>替换</b>会覆盖同名参数的值（不存在则新增），<b>删除</b>会移除该参数。多个替换项按从上到下顺序执行。
      </p>
      <el-table :data="replList" size="small" empty-text="暂无替换项，点击下方「添加一行」" max-height="320">
        <el-table-column label="启用" width="60" align="center">
          <template #default="{ row }">
            <el-switch v-model="row.enabled" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="类型" width="116" align="center">
          <template #default="{ row }">
            <el-select v-model="row.type" size="small">
              <el-option label="Query 参数" value="query" />
              <el-option label="请求头" value="header" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="108" align="center">
          <template #default="{ row }">
            <el-select v-model="row.action" size="small">
              <el-option label="替换/新增" value="set" />
              <el-option label="删除" value="del" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="参数名 (Key)" min-width="140">
          <template #default="{ row }">
            <el-input v-model="row.key" size="small" placeholder="如 token / Authorization" clearable />
          </template>
        </el-table-column>
        <el-table-column label="替换为 (Value)" min-width="150">
          <template #default="{ row }">
            <el-input v-model="row.value" size="small" :disabled="row.action === 'del'"
                      :placeholder="row.action === 'del' ? '删除时无需填写' : '新值'" clearable />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="66" align="center">
          <template #default="{ $index }">
            <el-button link type="danger" size="small" @click="removeRepl($index)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button size="small" @click="replDialog = false">完成</el-button>
        <el-button size="small" @click="addRepl">添加一行</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, onBeforeUnmount, onActivated } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  SniffStatus, SniffStart, SniffStop, SniffSetFilter, SniffListSessions,
  SniffGetSession, SniffDeleteSession, SniffExportOpenAPI, SniffInstallCA, SniffCAPEM,
  SniffSetSystemProxy, SniffGenerateApiFromSession, SniffGenerateApiFromRecords,
  SniffImportCA, SniffPickCAFile, SniffGetSessionErrors, SniffGetRewrites, SniffSetRewrites,
  CopyToClipboard, SendRequest
} from '../../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime'
import { store, reloadStore } from '../../store'
import GroupTreeNode from './GroupTreeNode.vue'

const status = reactive({ running: false, proxyAddr: '', certURL: '', localCertURL: '', caInstalled: false, caFingerprint: '', systemProxy: false, error: '', activeSessionId: '' })

// 手机证书信息默认隐藏，点击控制栏「抓包中」地址标签才展开（不挤压下方流量列表）。
const certOpen = ref(false)
function toggleCert() { certOpen.value = !certOpen.value }
const proxyAddr = ref('127.0.0.1:8888')
const sysProxy = ref(false)
const starting = ref(false)
const stopping = ref(false) // 是否正在执行“停止并保存”，用于按钮 loading 反馈
const flushFrozen = ref(false) // 停止后冻结实时渲染，释放主线程避免点击卡顿
const installing = ref(false)
const liveRecords = ref([])
const selected = ref(null)
const selectedIds = ref([])
const detailTab = ref('overview')
const liveFilter = ref('')
const viewMode = ref('list') // list / group
const trafficOpen = ref(true) // 实时流量板块是否展开（可折叠收起节省空间）
const showErrors = ref(true) // 是否显示解密失败/连接错误提示（关闭则隐藏错误列表、解决引导与弹窗提示，仅保留顶部「解密失败日志」计数）
// 分组模式下记录用户手动展开的节点 key（host 或目录），默认全部折叠
const expandedKeys = ref(new Set())
const onlyErr = ref(false) // 仅查看有解密失败记录的连接
const errTypeFilter = ref('') // 按错误类型筛选（pinning/untrusted/tls/connect/non_http，空=全部）
const errHostsByType = ref({}) // { type: Set<host> } 按类型收集解密失败 host

// selectedIds 对应的 Set 缓存，供列表/分组渲染 O(1) 判断是否勾选，避免大量 includes 数组扫描
const selectedIdSet = computed(() => new Set(selectedIds.value))

// 实时流量窗口内过滤（协议勾选/host/url/方法/仅异常/按类型）。
// 协议勾选在本地实时生效：勾选哪些协议就显示哪些，空=全部。
const filteredRecords = computed(() => {
  const kw = liveFilter.value.trim().toLowerCase()
  const only = onlyErr.value
  const type = errTypeFilter.value
  const protocols = filterProtocols.value
  const records = liveRecords.value
  const errByType = errHostsByType.value
  // 协议别名归一：勾选 http 也匹配 https；勾选 websocket 也匹配 wss（与后端一致）
  const aliasOf = { https: 'http', wss: 'websocket' }
  // 无任何过滤条件时直接返回，避免不必要的遍历
  if (!kw && !only && protocols.length === 0) return records
  return records.filter(r => {
    // 协议过滤
    if (protocols.length > 0) {
      const prot = (r.protocol || '').toLowerCase()
      const mapped = aliasOf[prot] || prot
      if (!protocols.includes(prot) && !protocols.includes(mapped)) return false
    }
    // 仅异常 + 错误类型过滤
    if (only && r.host) {
      if (type) {
        const hosts = errByType[type]
        if (!hosts || !hosts.has(r.host)) return false
      } else {
        let any = false
        for (const k in errByType) {
          if (errByType[k] && errByType[k].has(r.host)) { any = true; break }
        }
        if (!any) return false
      }
    }
    if (!kw) return true
    return (r.host && r.host.toLowerCase().includes(kw)) ||
      (r.url && r.url.toLowerCase().includes(kw)) ||
      (r.method && r.method.toLowerCase().includes(kw))
  })
})

// ---- Charles 风格分组树 ----
// 结构：host → 目录（按 URL 路径段分层）→ 请求。默认全部折叠，手动展开。
// 节点 key 规则：host 节点 "h|<host>"；目录节点 "h|<host>|<path>"；请求叶子就是记录本身。

// 从记录解析 host 与路径分段（不含 query）。
function pathSegsOf(r) {
  const host = r.host || '(未知)'
  if (r.path) return { host, segs: r.path.split('/').filter(Boolean) }
  if (r.url) {
    try {
      const u = new URL(r.url)
      return { host, segs: u.pathname.split('/').filter(Boolean) }
    } catch (e) { /* ignore */ }
  }
  return { host, segs: [] }
}

// 构建多级分组树。返回 top-level 节点数组。
const groupedRecords = computed(() => {
  // 平铺模式不构建分组树，避免流量刷新时全量重建
  if (viewMode.value !== 'group') return []
  const set = selectedIdSet.value
  const hostMap = {} // host -> 该 host 的目录树根
  // nodeIndex: "host\x00fullPath" -> 目录节点，O(1) 定位避免逐级 find 查找
  const nodeIndex = {}

  for (const r of filteredRecords.value) {
    const { host, segs } = pathSegsOf(r)
    let hostNode = hostMap[host]
    if (!hostNode) {
      hostNode = { key: 'h|' + host, host, name: host, type: 'host', level: 0, records: [], children: [] }
      hostMap[host] = hostNode
    }
    // 沿路径段逐级下钻（用 nodeIndex 直接命中目录节点），创建缺失的目录节点
    let cur = hostNode
    let fullPath = ''
    let level = 0
    for (const seg of segs) {
      fullPath += '/' + seg
      level += 1
      const idxKey = host + '\x00' + fullPath
      let node = nodeIndex[idxKey]
      if (!node) {
        node = { key: 'h|' + host + fullPath, host, seg, name: seg, type: 'dir', level, records: [], children: [] }
        nodeIndex[idxKey] = node
        cur.children.push(node)
      }
      cur = node
    }
    // 到达目录后，请求记录挂到该目录（或 host）下
    cur.records.push(r)
  }

  // 计算每个节点的展开态与勾选态
  const setState = (node) => {
    // 勾选态：根据自身 records 与子节点推导
    let all = node.records.length > 0
    let some = false
    for (const rec of node.records) {
      if (set.has(rec.id)) some = true
      else all = false
    }
    for (const child of node.children) {
      setState(child)
      if (child.allChecked) some = true
      else all = false
      if (child.someChecked) some = true
    }
    node.allChecked = all && (node.records.length > 0 || node.children.length > 0)
    node.someChecked = some && !node.allChecked
  }

  const roots = Object.values(hostMap)
  for (const root of roots) setState(root)
  return roots
})

// 展开/折叠节点
function toggleGroup(key) {
  const s = new Set(expandedKeys.value)
  if (s.has(key)) s.delete(key); else s.add(key)
  expandedKeys.value = s
}

// 收集节点下所有请求 id（含子目录）
function collectRecordIds(node, acc) {
  for (const rec of node.records) acc.push(rec.id)
  for (const child of node.children) collectRecordIds(child, acc)
}

// 勾选/取消勾选整个 host 或目录（含其下所有请求）
function toggleGroupSelect(node, val) {
  const set = new Set(selectedIds.value)
  const ids = []
  collectRecordIds(node, ids)
  ids.forEach(id => { if (val) set.add(id); else set.delete(id) })
  selectedIds.value = Array.from(set)
}

// 从 GroupTreeNode 冒泡的节点勾选事件：payload = { node, val }
function onGroupSelectNode(payload) {
  if (payload) toggleGroupSelect(payload.node, payload.val)
}

// 从 GroupTreeNode 冒泡的请求勾选事件：payload = { id, val }
function onSelectRecToggle(payload) {
  if (payload) toggleSelect(payload.id, payload.val)
}

const sessions = ref([])
const sessionsDialog = ref(false)
const caDialog = ref(false)
const caPem = ref('')
const importCADialog = ref(false)
const importCertPem = ref('')
const importKeyPem = ref('')
const importingCA = ref(false)
const pickingFile = ref(false)

// 请求/响应体格式化
const reqFormatted = ref(false)
const respFormatted = ref(false)

const filterHosts = ref('')
const filterExclude = ref('localhost, 127.0.0.1')
const filterOnlyHTTP = ref(false)
const filterProtocols = ref([]) // http/https/websocket/sse/grpc/graphql，空=全部解析
const autoDoc = ref(false)
const filterOpen = ref(false) // 过滤条件面板默认收起，避免挤压流量列表

// filterCount 当前生效的过滤条件数量（展示在收起状态的角标上）
const filterCount = computed(() => {
  let n = 0
  if (filterHosts.value.trim()) n++
  if (filterExclude.value.split(',').map(x => x.trim()).filter(Boolean).length) n++
  if (filterProtocols.value.length) n++
  if (filterOnlyHTTP.value) n++
  return n
})

// filterSummary 收起时的一行摘要，保证折叠后仍能看清当前过滤配置
const filterSummary = computed(() => {
  const parts = []
  const host = filterHosts.value.trim()
  if (host) parts.push('Host: ' + host)
  const ex = filterExclude.value.split(',').map(x => x.trim()).filter(Boolean)
  if (ex.length) parts.push('排除: ' + ex.join(', '))
  if (filterProtocols.value.length) parts.push('协议: ' + filterProtocols.value.join(' / '))
  if (filterOnlyHTTP.value) parts.push('仅 HTTP(S)')
  return parts.length ? parts.join(' · ') : '未设置（抓取全部流量）'
})

// 导入接口树
const importDialog = ref(false)
const importProjectId = ref(store.data.currentProjectId || '')
const importDirId = ref('')
const importing = ref(false)
const projectDirOptions = computed(() => {
  const p = store.data.projects.find(x => x.id === importProjectId.value)
  if (!p) return [{ value: '', label: '根目录' }]
  const nodes = (parentId) => p.dirs
    .filter(d => d.parentId === parentId)
    .sort((a, b) => (a.sort || 0) - (b.sort || 0))
    .map(d => ({ value: d.id, label: d.name, children: nodes(d.id) }))
  return [{ value: '', label: '根目录', children: nodes('') }]
})

let recOff = null
let statusOff = null
let errOff = null
let lastErrToastAt = 0 // 错误 toast 节流时间戳

// ---- 实时记录批量接收 + 限流渲染 ----
// 后端按 40ms 窗口批量推送，前端再做一次 rAF 合并，避免高频 push 触发逐条重渲染。
const MAX_LIVE = 1000 // 实时列表最多保留条数，防止内存无限增长
const MAX_RENDER = 400 // 平铺模式单次渲染上限：只渲染最新 N 条，避免大列表每次全量 DOM patch
// 平铺模式渲染列表（新记录在前，slice 保留最新）
const listRecords = computed(() => filteredRecords.value.slice(0, MAX_RENDER))
let pendingRecords = [] // 待合并到 liveRecords 的记录
let flushRaf = null // rAF 句柄
let lastFlushAt = 0 // 上次实际 flush 的时间戳，用于渲染节流

// 合并缓冲中的记录到 liveRecords，并限制列表长度（保留最新）。
function flushRecords() {
  flushRaf = null
  const batch = pendingRecords
  pendingRecords = []
  if (!batch.length) return
  // 渲染节流：流量高峰期每帧都有新 batch 时，仍保持至少 ~4fps 的合并渲染，
  // 避免每帧全量 patch 大列表把主线程占满，导致界面点击（如“停止并保存”）卡顿无响应
  const now = performance.now()
  if (now - lastFlushAt < 250) {
    flushRaf = requestAnimationFrame(flushRecords)
    return
  }
  lastFlushAt = now
  const cur = liveRecords.value
  // 新的记录放在最前面（最新置顶），超出上限时裁剪掉尾部最旧的记录
  if (cur.length + batch.length > MAX_LIVE) {
    liveRecords.value = batch.concat(cur).slice(0, MAX_LIVE)
  } else {
    liveRecords.value = batch.concat(cur)
  }
  // 自动生成文档：批量静默导入有效 HTTP 流量
  if (autoDoc.value) {
    const valid = batch.filter(r => r.method && r.url)
    if (valid.length) autoImport(valid)
  }
}

// 记录到达：先入缓冲，再由 rAF 统一合并渲染（天然限流）。
function onRecords(raw) {
  // 停止过程中冻结渲染：抓包已结束，残留事件无需再 patch 大列表，避免占用主线程
  if (flushFrozen.value) return
  const batch = Array.isArray(raw) ? raw : [raw]
  if (!batch.length) return
  for (const r of batch) {
    if (r && r.id) pendingRecords.push(r)
  }
  if (flushRaf == null) {
    flushRaf = requestAnimationFrame(flushRecords)
  }
}

// 停止抓包时立即冻结实时渲染循环，释放主线程，保证“停止并保存”点击即时响应
function freezeFlush() {
  if (flushRaf != null) {
    cancelAnimationFrame(flushRaf)
    flushRaf = null
  }
  pendingRecords = []
  flushFrozen.value = true
}

const errorList = ref([])
function nowStr() {
  const d = new Date()
  return d.toLocaleTimeString('zh-CN', { hour12: false })
}

const ERR_LABELS = {
  pinning: '证书固定',
  untrusted: 'CA未信任',
  tls: 'TLS握手失败',
  connect: '连接失败',
  non_http: '非HTTP协议',
}
function errLabel(type) { return ERR_LABELS[type] || '错误' }
function errTagType(type) {
  switch (type) {
    case 'pinning': return 'danger'
    case 'untrusted': return 'warning'
    case 'tls': return 'warning'
    case 'non_http': return 'info'
    default: return 'danger'
  }
}
function setLiveFilter(host) {
  if (host) { liveFilter.value = host; viewMode.value = 'list' }
}

// 错误类型筛选选项
const errTypeOptions = [
  { value: 'pinning', label: '证书固定' },
  { value: 'untrusted', label: 'CA未信任' },
  { value: 'tls', label: 'TLS握手失败' },
  { value: 'connect', label: '连接失败' },
  { value: 'non_http', label: '非HTTP协议' },
]

// ---- 解密失败日志弹窗（落盘数据） ----
const errorDialog = ref(false)
const errList = ref([]) // 来自后端的持久化错误记录（ErrorInfo[]）
const errCount = ref(0) // 当前会话失败条数
async function openErrors() {
  const sid = status.activeSessionId
  if (!sid) { ElMessage.info('暂无会话，请先开始抓包'); return }
  try {
    const list = await SniffGetSessionErrors(sid)
    errList.value = list || []
    errCount.value = errList.value.length
  } catch (e) {
    ElMessage.error('加载失败日志失败：' + (e || ''))
  }
  errorDialog.value = true
}
function errTypeText(type) { return ERR_LABELS[type] || '错误' }

// ---- 请求改写（域名重定向 + 参数替换） ----
const rewritesDialog = ref(false)
const rewrites = ref([])
const savingRewrites = ref(false)

// ---- 参数替换子弹窗（Query 参数 / 请求头的替换、新增、删除） ----
const replDialog = ref(false)
const replList = ref([])
const replTitle = ref('')
let replRow = null // 当前正在编辑替换项的规则对象（引用）

function newRewrite() {
  return {
    id: 'rw_' + Date.now() + '_' + Math.random().toString(36).slice(2, 8),
    from: '', to: '', enabled: true, desc: '', scheme: 'auto', replacements: [],
  }
}

function addRewrite() {
  rewrites.value.push(newRewrite())
}

function removeRewrite(i) {
  rewrites.value.splice(i, 1)
}

// replLabel 操作列「参数」按钮文案，带已有替换项数量。
function replLabel(row) {
  const n = (row.replacements || []).length
  return n ? `参数(${n})` : '参数'
}

function openRepl(row) {
  replRow = row
  replTitle.value = `${row.from || '未命名'} → ${row.to || '未填写'}`
  replList.value = (row.replacements || []).map(x => ({
    type: x.type === 'header' ? 'header' : 'query',
    action: x.action === 'del' ? 'del' : 'set',
    key: x.key || '', value: x.value || '', enabled: x.enabled !== false,
  }))
  replDialog.value = true
}

function addRepl() {
  replList.value.push({ type: 'query', action: 'set', key: '', value: '', enabled: true })
}

function removeRepl(i) {
  replList.value.splice(i, 1)
}

// 关闭子弹窗时把替换项写回所属规则（丢弃未填参数名的空行）。
// 采用引用写入，因此无需额外保存动作，随主弹窗「保存」一起提交。
watch(replDialog, v => {
  if (v || !replRow) return
  replRow.replacements = replList.value
    .filter(x => (x.key || '').trim())
    .map(x => ({
      type: x.type, action: x.action,
      key: x.key.trim(), value: x.value || '', enabled: !!x.enabled,
    }))
  replRow = null
})

async function openRewrites() {
  try {
    const list = await SniffGetRewrites()
    rewrites.value = (list || []).map(r => ({
      id: r.id, from: r.from || '', to: r.to || '', enabled: r.enabled !== false, desc: r.desc || '',
      scheme: r.scheme || 'auto',
      replacements: (r.replacements || []).map(x => ({
        type: x.type === 'header' ? 'header' : 'query',
        action: x.action === 'del' ? 'del' : 'set',
        key: x.key || '', value: x.value || '', enabled: x.enabled !== false,
      })),
    }))
  } catch (e) {
    ElMessage.error('加载改写配置失败：' + String(e))
    rewrites.value = []
  }
  rewritesDialog.value = true
}

async function saveRewrites() {
  const list = rewrites.value
  for (const r of list) {
    if (!r.from || !r.from.trim()) { ElMessage.warning('请填写原域名 (From)'); return }
    if (!r.to || !r.to.trim()) { ElMessage.warning('请填写改写地址 (To)'); return }
  }
  savingRewrites.value = true
  try {
    await SniffSetRewrites(list.map(r => ({
      id: r.id, from: r.from.trim(), to: r.to.trim(), enabled: !!r.enabled, desc: (r.desc || '').trim(),
      scheme: r.scheme || 'auto',
      replacements: (r.replacements || []).map(x => ({
        type: x.type, action: x.action, key: x.key, value: x.value || '', enabled: !!x.enabled,
      })),
    })))
    ElMessage.success('已保存，改写配置即时生效')
    rewritesDialog.value = false
  } catch (e) {
    ElMessage.error('保存失败：' + String(e))
  } finally {
    savingRewrites.value = false
  }
}

// 各错误类型的解决引导
const GUIDES = {
  pinning: {
    type: 'pinning',
    solution: '目标 App/网站内置了证书固定（Certificate Pinning），即使信任根证书也会拒绝伪造证书。换任何代理/证书都无法直接解密。解决办法：1) 如为目标 App，可尝试使用 Fiddler 的证书固定绕过插件或安卓的 JustTrustMe/SSLUnpinning 等（需配合测试环境）；2) 若为第三方 SDK，查看其是否可关闭 pinning；3) 该流量只能透传查看 IP/端口，无法查看明文 body。',
  },
  untrusted: {
    type: 'untrusted',
    solution: '根证书未受系统信任，HTTPS 被降级为透传。解决办法：1) 以管理员身份运行 ApiTool；2) 点击「安装根证书」（certutil -addstore Root）；3) 重启浏览器/目标应用刷新证书缓存；4) 确认界面显示「CA 已安装」。',
    action: { label: '重新安装根证书', fn: () => installCA() },
  },
  tls: {
    type: 'tls',
    solution: 'TLS 握手失败，常见于证书固定或客户端校验。若错误同时提示证书相关（x509），请先确认根证书已安装并信任；若为 App 内证书校验，同「证书固定」处理。',
  },
  connect: {
    type: 'connect',
    solution: '连接目标失败（connectex/refused/timeout）。解决办法：1) 确认目标地址/端口可达（ping/telnet）；2) 确认走代理的应用未使用直连或 VPN；3) 该主机可能不支持 HTTP(S) 标准端口。',
  },
  non_http: {
    type: 'non_http',
    solution: '该连接为 SSH/FTP/自定义二进制等非 HTTP 协议，当前代理仅透传、不解密。如需解析需扩展协议支持（当前版本不支持）。',
  },
}
const currentGuide = computed(() => {
  const type = errTypeFilter.value
  if (type && GUIDES[type]) return GUIDES[type]
  // 未按类型筛选时，取最近一条有分类的错误作引导
  const last = errorList.value.find(e => GUIDES[e.type])
  return last ? GUIDES[last.type] : null
})

onMounted(async () => {
  try {
    const s = await SniffStatus()
    Object.assign(status, s)
    sysProxy.value = !!s.systemProxy
  } catch (e) { /* ignore */ }
  try { sessions.value = await SniffListSessions() } catch (e) {}
  recOff = EventsOn('sniff:record', onRecords)
  statusOff = EventsOn('sniff:status', (s) => { Object.assign(status, s); sysProxy.value = !!s.systemProxy; if (s.activeSessionId) status.activeSessionId = s.activeSessionId })
  errOff = EventsOn('sniff:error', (info) => {
    const obj = typeof info === 'string' ? { type: 'connect', host: '', message: info } : info
    const text = (obj && obj.message) || 'HTTPS 解密异常，请确认根证书已安装并信任'
    errorList.value.push({ time: nowStr(), type: (obj && obj.type) || 'connect', host: (obj && obj.host) || '', msg: text })
    if (errorList.value.length > 50) errorList.value = errorList.value.slice(-50)
    errCount.value += 1
    // 按类型收集解密失败的 host
    if (obj && obj.host && obj.type) {
      const m = { ...errHostsByType.value }
      const s = new Set(m[obj.type] || [])
      s.add(obj.host)
      m[obj.type] = s
      errHostsByType.value = m
    }
    // 错误 toast 节流：大量解密失败时避免瞬间弹出几十条全局提示卡死 UI（错误已入列表展示）
    const _n = Date.now()
    if (showErrors.value && _n - lastErrToastAt > 2000) {
      lastErrToastAt = _n
      ElMessage.warning(text)
    }
  })
})

// 切回该页（keep-alive 激活）时异步刷新状态与会话列表，确保数据最新。
// 注意：不重置 liveRecords，已抓流量快照由 keep-alive 保留，这里仅做增量刷新。
onActivated(async () => {
  try {
    const s = await SniffStatus()
    Object.assign(status, s)
    sysProxy.value = !!s.systemProxy
  } catch (e) { /* ignore */ }
  try { sessions.value = await SniffListSessions() } catch (e) {}
})

onBeforeUnmount(() => {
  if (flushRaf != null) {
    cancelAnimationFrame(flushRaf)
    flushRaf = null
  }
  pendingRecords = []
  if (recOff) EventsOff('sniff:record', recOff)
  if (statusOff) EventsOff('sniff:status', statusOff)
  if (errOff) EventsOff('sniff:error', errOff)
})

function setSysProxy(val) {
  SniffSetSystemProxy(!!val).catch(() => {})
}

async function startSniff() {
  starting.value = true
  flushFrozen.value = false // 重新开始抓包，恢复实时渲染
  try {
    applyFilter()
    const addr = proxyAddr.value.trim() || '127.0.0.1:8888'
    await SniffStart(addr)
    liveRecords.value = []
    if (!status.caInstalled) {
      ElMessage.warning('已启动（仅 HTTP 明文）。解密 HTTPS 请先安装根证书')
    } else {
      ElMessage.success('抓包已启动，监听 ' + (status.proxyAddr || addr) + '，系统流量将通过代理经过本工具')
    }
  } catch (e) {
    ElMessage.error('启动失败：' + String(e))
  } finally {
    starting.value = false
  }
}

async function stopSniff() {
  if (stopping.value) return
  stopping.value = true
  // 先冻结实时渲染并释放主线程，确保后续 SniffStop / 列表刷新不被大列表 patch 阻塞
  freezeFlush()
  try {
    // SniffStop 后端立即返回（落盘在后台 goroutine），秒回
    await SniffStop()
    ElMessage.success('已停止并保存会话')
    // 列表刷新可能读取较多历史会话，放最后且不阻塞“已停止”反馈
    sessions.value = await SniffListSessions()
  } catch (e) {
    ElMessage.error('停止失败：' + String(e))
  } finally {
    stopping.value = false
  }
}

function applyFilter() {
  const f = {
    host: filterHosts.value,
    excludeHosts: filterExclude.value.split(',').map(x => x.trim()).filter(Boolean),
    onlyHttp: filterOnlyHTTP.value,
    protocols: filterProtocols.value,
  }
  SniffSetFilter(f).catch(() => {})
}

async function installCA() {
  installing.value = true
  try {
    await SniffInstallCA()
    status.caInstalled = true
    ElMessage.success('根证书已安装，现在可解密 HTTPS')
  } catch (e) {
    ElMessage.error('安装失败：' + String(e) + '\n请尝试以管理员身份运行本程序，或手动导入 ca.pem')
  } finally {
    installing.value = false
  }
}

async function openCADialog() {
  try { caPem.value = await SniffCAPEM() } catch (e) { caPem.value = '' }
  caDialog.value = true
}

function openImportCADialog() {
  importCertPem.value = ''
  importKeyPem.value = ''
  importCADialog.value = true
}

async function pickCAFile() {
  pickingFile.value = true
  try {
    const res = await SniffPickCAFile()
    if (!res) return
    if (res.certPem) importCertPem.value = res.certPem
    if (res.keyPem) {
      importKeyPem.value = res.keyPem
    } else {
      importKeyPem.value = ''
      ElMessage.warning('所选文件未包含私钥。若为 FiddlerRoot.cer（仅证书），请补充私钥 PEM，或从 Fiddler 导出含私钥的证书')
    }
  } catch (e) {
    ElMessage.error('读取证书失败：' + String(e))
  } finally {
    pickingFile.value = false
  }
}

async function doImportCA() {
  if (!importCertPem.value.trim() || !importKeyPem.value.trim()) {
    ElMessage.warning('请同时填写证书 PEM 与私钥 PEM')
    return
  }
  importingCA.value = true
  try {
    const fp = await SniffImportCA(importCertPem.value, importKeyPem.value)
    Object.assign(status, { caFingerprint: fp, caInstalled: false })
    ElMessage.success('已导入根证书（指纹 ' + fp + '）。如需解密 HTTPS，请点击「安装根证书」安装到系统信任库')
    importCADialog.value = false
  } catch (e) {
    ElMessage.error('导入失败：' + String(e))
  } finally {
    importingCA.value = false
  }
}

function selectRecord(r) { selected.value = r; detailTab.value = 'overview' }

// ---- 右键上下文菜单 ----
const ctxMenu = ref(null) // { x, y, rec }

function openCtxMenu(recOrPayload, ev) {
  let rec, e2
  if (recOrPayload && recOrPayload.rec) { rec = recOrPayload.rec; e2 = recOrPayload.event }
  else { rec = recOrPayload; e2 = ev }
  if (!rec) return
  ctxMenu.value = { x: e2.clientX, y: e2.clientY, rec }
}
function closeCtxMenu() { ctxMenu.value = null }

// 生成 bash/curl 命令（单引号包裹并转义内部单引号）
function bashQuote(s) {
  if (s == null) return "''"
  return "'" + String(s).replace(/'/g, "'\\''") + "'"
}
function buildCurl(rec) {
  const lines = [`curl -X ${rec.method || 'GET'} ${bashQuote(rec.url)}`]
  for (const h of (rec.reqHeaders || [])) {
    if (h && h.disabled) continue
    const k = h.key != null ? h.key : h.name
    const v = h.value != null ? h.value : h.val
    if (!k && !v) continue
    lines.push(`  -H ${bashQuote(`${k}: ${v}`)}`)
  }
  const body = rec.reqBody
  if (body && String(body).length) {
    lines.push(`  --data-raw ${bashQuote(body)}`)
  }
  return lines.join(' \\\n')
}
function buildRawHttp(rec) {
  const head = [`${rec.method || 'GET'} ${rec.url} HTTP/1.1`]
  for (const h of (rec.reqHeaders || [])) {
    if (h && h.disabled) continue
    const k = h.key != null ? h.key : h.name
    const v = h.value != null ? h.value : h.val
    if (!k && !v) continue
    head.push(`${k}: ${v}`)
  }
  let out = head.join('\r\n') + '\r\n'
  if (rec.reqBody) out += '\r\n' + rec.reqBody
  return out
}

function ctxReplay() {
  const rec = ctxMenu.value.rec
  closeCtxMenu()
  let bodyType = rec.reqBodyType
  if (!bodyType || bodyType === 'none') {
    if (rec.reqBody) bodyType = /^\s*[{\[]/.test(rec.reqBody) ? 'json' : 'text'
    else bodyType = 'none'
  }
  const spec = {
    method: rec.method,
    url: rec.url,
    headers: rec.reqHeaders,
    bodyType: bodyType,
    body: rec.reqBody || '',
    timeoutSec: 30,
  }
  const loading = ElMessage({ message: '重放请求中…', type: 'info', duration: 0 })
  SendRequest(spec).then(r => {
    loading.close()
    const headers = (r.headers && Object.keys(r.headers).length)
      ? Object.entries(r.headers).map(([k, v]) => ({ key: k, value: v, enabled: true }))
      : []
    const replayed = {
      ...rec,
      statusCode: r.status,
      statusText: r.statusText,
      respHeaders: headers,
      respBody: r.error ? ('请求失败：' + r.error) : r.body,
      respBodyType: r.isJson ? 'json' : 'text',
      respContentType: (r.headers && r.headers['Content-Type']) || '',
      durationMs: r.durationMs,
      note: r.error ? ('重放失败 · ' + r.error) : ('重放成功 · ' + new Date().toLocaleTimeString()),
      reqClipped: false,
      respClipped: false,
    }
    selected.value = replayed
    detailTab.value = 'resb'
    if (r.error) ElMessage.error('重放请求失败：' + r.error)
    else ElMessage.success('重放成功 · ' + r.status + ' · ' + r.durationMs + 'ms')
  }).catch(e => {
    loading.close()
    ElMessage.error('重放请求异常：' + (e && e.message ? e.message : e))
  })
}
function ctxCopyAddr() { copyText(ctxMenu.value.rec.url); closeCtxMenu() }
function ctxCopyCurl() { copyText(buildCurl(ctxMenu.value.rec)); closeCtxMenu() }
function ctxCopyRawHttp() { copyText(buildRawHttp(ctxMenu.value.rec)); closeCtxMenu() }
function ctxCopyReqHeaders() { copyText(kvToText(ctxMenu.value.rec.reqHeaders)); closeCtxMenu() }
function ctxCopyReqBody() { copyText(displayBody(ctxMenu.value.rec.reqBody, true)); closeCtxMenu() }
function ctxCopyResBody() { copyText(displayBody(ctxMenu.value.rec.respBody, true)); closeCtxMenu() }
function toggleSelect(id, val) {
  const set = new Set(selectedIds.value)
  if (val) set.add(id)
  else set.delete(id)
  selectedIds.value = Array.from(set)
}

function selectAll() {
  // 全选当前过滤后的记录
  const set = new Set(selectedIds.value)
  filteredRecords.value.forEach(r => set.add(r.id))
  selectedIds.value = Array.from(set)
}
function clearLive() {
  if (flushRaf != null) { cancelAnimationFrame(flushRaf); flushRaf = null }
  pendingRecords = []
  liveRecords.value = []; selected.value = null; selectedIds.value = []
  errCount.value = 0
}

function selectedRecords() {
  const set = selectedIdSet.value
  return liveRecords.value.filter(r => set.has(r.id))
}

function openBatchImport() {
  if (!selectedIds.value.length) {
    ElMessage.warning('请先勾选要导入的流量记录')
    return
  }
  importProjectId.value = store.data.currentProjectId || ''
  importDirId.value = ''
  importDialog.value = true
}

// 实际执行导入（批量或单条）
async function doImportApi() {
  const batch = selectedRecords()
  if (batch.length) {
    await doBatchImport(batch)
    return
  }
  // 单条：走会话导入（兼容从会话打开的记录）
  if (selected.value && selected.value.sessionId) {
    const rec = selected.value
    importing.value = true
    try {
      const n = await SniffGenerateApiFromSession(rec.sessionId, [rec.id], importProjectId.value, importDirId.value)
      await reloadStore()
      ElMessage.success(`已生成并导入 ${n} 个接口`)
      importDialog.value = false
    } catch (e) {
      ElMessage.error(String(e))
    } finally {
      importing.value = false
    }
    return
  }
  if (selected.value) {
    await doBatchImport([selected.value])
    return
  }
  ElMessage.warning('请先选择或勾选要导入的流量记录')
}

async function doBatchImport(records) {
  if (!records.length) return
  importing.value = true
  try {
    const n = await SniffGenerateApiFromRecords(records, importProjectId.value, importDirId.value)
    await reloadStore()
    const p = store.data.projects.find(x => x.id === importProjectId.value)
    ElMessage.success(`已生成并导入 ${n} 个接口到「${p?.name || ''}」`)
    importDialog.value = false
    selectedIds.value = []
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    importing.value = false
  }
}

// 自动生成（autoDoc）：静默导入，失败不打扰
async function autoImport(records) {
  try {
    const pid = store.data.currentProjectId
    if (!pid) return
    await SniffGenerateApiFromRecords(records, pid, '')
    await reloadStore()
  } catch (e) { /* 自动生成失败静默 */ }
}

function kvToText(kvs) {
  if (!kvs || !kvs.length) return '（无）'
  return kvs.filter(k => k.enabled !== false).map(k => k.key + ': ' + k.value).join('\n')
}

// 将 RFC3339 时间戳格式化为「年-月-日 时:分:秒」
function formatTime(ts) {
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ts
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

async function refreshSessions() {
  try { sessions.value = await SniffListSessions() } catch (e) {}
}

async function exportSession(id, name) {
  try {
    const path = await SniffExportOpenAPI(id, name || 'openapi')
    if (path) ElMessage.success('已导出：' + path)
    else ElMessage.info('已取消导出')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function deleteSession(id) {
  try {
    await ElMessageBox.confirm('确定删除该抓包会话？', '提示', { type: 'warning' })
    await SniffDeleteSession(id)
    sessions.value = await SniffListSessions()
    ElMessage.success('已删除')
  } catch (e) { if (e !== 'cancel') ElMessage.error(String(e)) }
}

// 打开会话面板（弹窗打开时即刷新列表），不占用主抓包界面
function openSessions() {
  sessionsDialog.value = true
  refreshSessions()
}

// 一键清除全部会话包
async function clearAllSessions() {
  try {
    if (!sessions.value.length) return
    await ElMessageBox.confirm(`确定清除全部 ${sessions.value.length} 个抓包会话？此操作不可恢复。`, '提示', { type: 'warning' })
    for (const s of sessions.value) {
      await SniffDeleteSession(s.id)
    }
    sessions.value = []
    ElMessage.success('已清除全部会话')
  } catch (e) { if (e !== 'cancel') ElMessage.error(String(e)) }
}

function openImportDialog() {
  if (!selected.value) {
    ElMessage.warning('请先选择一条流量记录')
    return
  }
  importProjectId.value = store.data.currentProjectId || ''
  importDirId.value = ''
  importDialog.value = true
}

function copyText(t) {
  if (!t) return
  CopyToClipboard(t).catch(() => {})
}

// ---- 请求/响应体格式化 ----
function toggleFormat(kind) {
  if (kind === 'req') reqFormatted.value = !reqFormatted.value
  else respFormatted.value = !respFormatted.value
}

function displayBody(text, formatted) {
  if (!text) return text || '（无）'
  // 超大 body 不做 JSON 格式化，避免 JSON.parse 大字符串阻塞主线程
  if (text.length > 2 * 1024 * 1024) return '（内容过大，已省略展示，可在会话历史中查看完整内容）'
  if (!formatted) return text
  // 尝试 JSON 美化
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch (e) { /* 非 JSON，原文 */ }
  return text
}

// ---- 响应图片预览 ----
function isImageResp(r) {
  if (!r || !r.respBody) return false
  const ct = (r.respContentType || '').toLowerCase()
  if (ct && ct.startsWith('image/')) return true
  // 无 content-type 时按 base64 图片头判断
  const b = r.respBody
  return /^(\/9j\/|iVBOR|R0lGOD|SUkq|data:image\/)/i.test(b)
}

function imgSrc(body) {
  if (!body) return ''
  // 已是 data URL 直接用
  if (body.startsWith('data:image/')) return body
  // 取干净的 MIME 类型（去掉 ; 后参数）
  const rawCT = (selected.value && selected.value.respContentType) || 'image/png'
  const mime = (rawCT.split(';')[0] || 'image/png').trim().toLowerCase()
  // 去除可能的空白换行后作为 base64
  const cleaned = body.replace(/\s+/g, '')
  return `data:${mime};base64,${cleaned}`
}

async function copyProxyAddr() {
  const addr = status.proxyAddr || proxyAddr.value
  if (!addr) {
    ElMessage.warning('暂无代理地址')
    return
  }
  try {
    await CopyToClipboard(addr)
    ElMessage.success('已复制代理地址：' + addr)
  } catch (e) {
    ElMessage.error('复制失败：' + String(e))
  }
}
</script>

<style scoped>
.mitm-panel {
  --mp-primary: #165dff; --mp-primary-soft: #e8f3ff; --mp-radius: 12px;
  --mp-bg: #f5f7fa; --mp-border: #eaecef; --mp-text: #1d2129; --mp-sub: #86909c;
  flex: 1; width: 100%; min-width: 0; display: flex; flex-direction: column; height: 100%; min-height: 0;
  padding: 16px 18px; gap: 12px; box-sizing: border-box; overflow: hidden;
  background: var(--mp-bg); color: var(--mp-text);
}
.mitm-top {
  display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 10px;
  padding: 14px 16px; background: #fff; border: 1px solid var(--mp-border); border-radius: var(--mp-radius);
  box-shadow: 0 1px 3px rgba(0, 21, 41, .04);
}
.mitm-title { display: flex; align-items: baseline; gap: 8px; }
.mitm-title h2 {
  margin: 0; font-size: 18px; font-weight: 700; letter-spacing: .3px;
  background: linear-gradient(90deg, #165dff, #4080ff); -webkit-background-clip: text; background-clip: text; color: transparent;
}
.mitm-title .sub { color: var(--mp-sub); font-size: 12px; }
.mitm-actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.mitm-body { display: flex; gap: 12px; flex: 1; min-height: 0; overflow: hidden; }
.mitm-left { flex: 0 0 42%; width: 42%; min-width: 40%; max-width: 62%; display: flex; flex-direction: column; gap: 10px; min-height: 0; contain: layout style; }
.mitm-right { flex: 1 1 auto; min-width: 0; border-left: 1px solid var(--mp-border); padding-left: 14px; min-height: 0; display: flex; flex-direction: column; overflow: hidden; contain: layout style; }
.mitm-right .detail-empty { flex: 1; display: flex; align-items: center; justify-content: center; }
/* 标题右侧「功能介绍」下拉入口：默认只占一小块，点击才展开说明，不挤压操作区 */
.mitm-title .sub-more {
  font-size: 12px; color: var(--mp-sub); cursor: pointer; user-select: none; white-space: nowrap;
  padding: 2px 6px; border-radius: 6px; transition: background .15s, color .15s;
}
.mitm-title .sub-more:hover { background: #f2f3f5; color: #165dff; }
.intro-box .intro-line { font-size: 13px; line-height: 1.9; color: #4e5969; }
.intro-box .intro-line b { color: #1d2129; }
.intro-box .intro-tip { margin-top: 8px; font-size: 12px; color: #86909c; line-height: 1.7; }

/* 过滤条件：默认收起为一行标题 + 条件摘要，点击展开完整表单 */
.filter-box {
  border: 1px solid var(--mp-border); border-radius: var(--mp-radius); padding: 12px;
  background: #fff; box-shadow: 0 1px 3px rgba(0, 21, 41, .04);
}
.filter-box.is-collapsed { padding: 8px 12px; }
.filter-head {
  display: flex; align-items: center; gap: 6px; cursor: pointer; user-select: none;
  font-size: 13px; font-weight: 600; color: var(--mp-text); min-width: 0;
}
.filter-head:hover .fh-title { color: #165dff; }
.filter-head .fh-caret { color: var(--mp-sub); font-size: 11px; width: 10px; flex-shrink: 0; }
.filter-head .fh-title { flex-shrink: 0; transition: color .15s; }
.filter-head .fh-badge { flex-shrink: 0; padding: 0 5px; height: 18px; line-height: 16px; }
.filter-head .fh-summary {
  flex: 1; min-width: 0; font-weight: 400; font-size: 12px; color: var(--mp-sub);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.filter-head .fh-hint { flex-shrink: 0; font-weight: 400; font-size: 12px; color: #165dff; }
.filter-body { display: flex; flex-direction: column; gap: 10px; margin-top: 10px; }
.filter-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.filter-row .fl { font-size: 12px; color: var(--mp-sub); white-space: nowrap; flex-shrink: 0; }
.traffic-head { display: flex; justify-content: space-between; align-items: center; font-weight: 600; font-size: 13px; color: var(--mp-text); }
.traffic-list {
  flex: 1; overflow: auto; border: 1px solid var(--mp-border); border-radius: var(--mp-radius); min-height: 120px;
  background: #fff; contain: layout style paint; box-shadow: 0 1px 3px rgba(0, 21, 41, .04); padding: 4px;
}
.traffic-item {
  display: flex; gap: 8px; align-items: center; padding: 8px 12px; cursor: pointer; font-size: 13px;
  border-radius: 9px; transition: background .12s ease; border: 1px solid transparent;
}
.traffic-item:hover { background: #f2f7ff; }
.traffic-head .render-limit { font-style: normal; font-size: 12px; color: #f76707; font-weight: 400; margin-left: 6px; }
.clipped-tip { font-size: 12px; color: #f76707; background: #fff7e8; border: 1px solid #ffd25e; border-radius: 6px; padding: 4px 8px; margin-bottom: 8px; }
.traffic-item.active { background: var(--mp-primary-soft); border-color: #bcd4ff; box-shadow: inset 3px 0 0 var(--mp-primary); }
.traffic-item.checked { background: #f0f6ff; }
.traffic-head .th-actions { display: flex; gap: 4px; align-items: center; }
/* 协议/方法/状态码 彩色徽章 */
.traffic-item .proto, .traffic-item .method, .traffic-item .status {
  font-size: 11px; font-weight: 700; padding: 2px 7px; border-radius: 6px; flex-shrink: 0; letter-spacing: .2px;
}
.traffic-item .proto { color: #fff; background: #86909c; }
.traffic-item .p-https { background: #165dff; }
.traffic-item .p-http { background: #00b42a; }
.traffic-item .p-websocket { background: #722ed1; }
.traffic-item .p-sse { background: #f76707; }
.traffic-item .p-tls { background: #ff7d00; }
.traffic-item .p-ssh, .traffic-item .p-ftp, .traffic-item .p-smtp { background: #eb0aa6; }
.traffic-item .method { color: #165dff; background: #e8f3ff; }
.traffic-item .m-GET { color: #00b42a; background: #e8ffea; }
.traffic-item .m-POST { color: #165dff; background: #e8f3ff; }
.traffic-item .m-PUT { color: #ff7d00; background: #fff3e8; }
.traffic-item .m-DELETE { color: #f53f3f; background: #ffece8; }
.traffic-item .m-PATCH { color: #722ed1; background: #f3edff; }
.traffic-item .m-HEAD, .traffic-item .m-OPTIONS { color: #86909c; background: #f2f3f5; }
.traffic-item .status { color: #86909c; background: #f2f3f5; }
.traffic-item .s-2 { color: #00b42a; background: #e8ffea; }
.traffic-item .s-3 { color: #165dff; background: #e8f3ff; }
.traffic-item .s-4 { color: #ff7d00; background: #fff3e8; }
.traffic-item .s-5 { color: #f53f3f; background: #ffece8; }
.traffic-item .url { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #1d2129; }

/* 右键菜单 */
.ctx-mask { position: fixed; inset: 0; z-index: 3000; }
.ctx-menu {
  position: fixed; min-width: 168px; background: #fff; border-radius: 10px;
  box-shadow: 0 6px 24px rgba(0, 0, 0, .14); padding: 5px; font-size: 13px; color: #1d2129;
}
.ctx-item { padding: 7px 12px; border-radius: 7px; cursor: pointer; white-space: nowrap; }
.ctx-item:hover { background: #f2f7ff; color: #165dff; }
.ctx-sep { height: 1px; background: #f2f3f5; margin: 4px 2px; }
.empty, .detail-empty { color: #c9cdd4; font-size: 13px; text-align: center; padding: 32px 0; }
.detail-head {
  display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 10px; flex-wrap: wrap;
  padding: 10px 12px; background: #fff; border: 1px solid var(--mp-border); border-radius: var(--mp-radius);
  box-shadow: 0 1px 3px rgba(0, 21, 41, .04);
}
.detail-head .dh-meta { display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; }
.detail-head .dh-actions { display: flex; gap: 8px; flex-shrink: 0; }
.detail-head .du {
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; color: #4e5969;
  max-width: 520px; background: #f2f3f5; padding: 3px 8px; border-radius: 6px;
}
.detail-tabs { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.detail-tabs .el-tabs__content { flex: 1; overflow: auto; min-height: 0; }
.kv { display: flex; gap: 10px; padding: 6px 0; font-size: 13px; border-bottom: 1px dashed #f2f3f5; }
.kv b { color: var(--mp-sub); width: 80px; font-weight: 500; }
.code {
  background: #fbfcfe; border: 1px solid var(--mp-border); border-radius: 10px; padding: 12px;
  font-size: 12px; line-height: 1.6; white-space: pre; word-break: normal; overflow: auto; max-height: 100%;
}
/* 详情区标签栏占满高度，内容超出时出现左右/上下滚动条而非被隐藏 */
.mitm-right .el-tabs { display: flex; flex-direction: column; flex: 1; min-height: 0; }
.mitm-right .el-tabs__header { flex: 0 0 auto; margin-bottom: 8px; }
.mitm-right .el-tabs__content { flex: 1; min-height: 0; }
.mitm-right .el-tab-pane { height: 100%; min-height: 0; display: flex; flex-direction: column; }
.mitm-right .el-tab-pane .code { flex: 1; min-height: 0; max-height: none; }
.mitm-right .el-tab-pane > .body-toolbar + .code { flex: 1; }
.ca { max-height: 240px; }
.body-toolbar { display: flex; justify-content: flex-end; margin-bottom: 4px; min-height: 20px; }
.mitm-errors { margin: 6px 0; display: flex; flex-direction: column; gap: 4px; }
.mitm-errors .err-item { --el-alert-padding: 6px 10px; }

/* 解密失败日志弹窗 */
.err-badge { margin-left: 4px; background: #f53f3f; color: #fff; border-radius: 10px; padding: 0 6px; font-size: 11px; line-height: 16px; }
.err-list { display: flex; flex-direction: column; gap: 10px; max-height: 60vh; overflow: auto; padding-right: 4px; }
.err-item { border: 1px solid var(--mp-border); border-radius: 10px; padding: 8px 10px; background: #fff; }
.err-row { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
.err-type { font-size: 11px; font-weight: 600; padding: 1px 8px; border-radius: 8px; }
.err-type.et-pinning { color: #cb2634; background: #ffece8; }
.err-type.et-untrusted { color: #d25f00; background: #fff4e3; }
.err-type.et-tls { color: #d25f00; background: #fff4e3; }
.err-type.et-connect { color: #cb2634; background: #ffece8; }
.err-type.et-non_http { color: #1d4ed8; background: #e8f0ff; }
.err-host { font-size: 12px; color: #4e5969; word-break: break-all; }
.err-msg { margin: 0; background: #fbfcfe; border: 1px solid var(--mp-border); border-radius: 8px; padding: 8px; font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-all; max-height: 200px; overflow: auto; }

/* 手机证书下载卡片 */
.addr-tag { cursor: pointer; transition: filter .12s ease; }
.addr-tag:hover { filter: brightness(1.08); }
.cert-card { margin: 8px 0; border: 1px solid #e8f3ff; border-radius: var(--mp-radius); background: linear-gradient(180deg, #f7fbff, #fff); box-shadow: 0 1px 3px rgba(22, 93, 255, .06); }
.cert-head { display: flex; align-items: center; justify-content: space-between; font-weight: 600; color: var(--mp-primary); }
.cert-body { display: flex; flex-direction: column; gap: 8px; }
.cert-row { display: flex; align-items: center; gap: 8px; }
.cert-label { flex-shrink: 0; width: 60px; color: #4e5969; font-size: 12px; }
.cert-url { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; background: #f2f3f5; border-radius: 6px; padding: 4px 8px; font-size: 12px; color: #1d2129; }
.cert-steps { margin: 4px 0 0; padding-left: 18px; color: #4e5969; font-size: 12px; line-height: 1.9; }
.mitm-errors .err-head { display: flex; align-items: center; gap: 8px; font-size: 12px; }
.mitm-errors .err-time { color: #86909c; }
.mitm-errors .err-tag { flex-shrink: 0; }
.mitm-errors .err-host { font-size: 12px; color: var(--mp-primary); cursor: pointer; }
.mitm-errors .err-host:hover { text-decoration: underline; }
.mitm-errors .err-body { font-size: 12px; color: #4e5969; word-break: break-all; margin-top: 2px; font-family: monospace; }
.mitm-errors .err-foot { display: flex; align-items: center; justify-content: space-between; font-size: 12px; color: #86909c; padding: 0 4px; }
.grp-item { padding-left: 22px; }
.traffic-head .th-actions .el-radio-group { margin-right: 4px; }
.traffic-head { cursor: pointer; user-select: none; }
.traffic-head .th-caret { display: inline-block; width: 14px; color: var(--mp-primary); font-size: 12px; }
.traffic-head .th-title { font-weight: 600; color: var(--mp-text); }
.traffic-head .th-actions { cursor: default; }
.mitm-guide { margin: 6px 0; padding: 10px 12px; border-radius: var(--mp-radius); font-size: 13px; line-height: 1.7; }
.mitm-guide.g-pinning { background: #fef0f0; border: 1px solid #fbc4c4; }
.mitm-guide.g-untrusted { background: #fdf6ec; border: 1px solid #f3d19e; }
.mitm-guide.g-tls { background: #fdf6ec; border: 1px solid #f3d19e; }
.mitm-guide.g-connect { background: #fef0f0; border: 1px solid #fbc4c4; }
.mitm-guide.g-non_http { background: #f4f4f5; border: 1px solid #e5e6eb; }
.mitm-guide .g-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; }
.mitm-guide .g-title { font-weight: 600; color: #1d2129; }
.mitm-guide .g-body { color: #4e5969; word-break: break-word; }
.mitm-guide .g-act { margin-top: 8px; }
.img-preview { background: #f7f8fa; border-radius: 8px; padding: 10px; text-align: center; }
.img-preview img { max-width: 100%; max-height: 460px; object-fit: contain; border-radius: 4px; }
.sess-toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
.sess-toolbar .sess-count { color: #4e5969; font-size: 13px; }
.import-row { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
.import-row .ir-label { width: 70px; color: #4e5969; font-size: 13px; flex-shrink: 0; }

/* 空状态图标 */
.empty-ico { font-size: 26px; margin-bottom: 6px; opacity: .7; }

/* 请求改写（域名重定向 + 参数替换）弹窗 */
.rw-tip { color: #4e5969; font-size: 13px; line-height: 1.7; margin: 0 0 12px; }
.rw-tip code { background: #f2f3f5; border-radius: 5px; padding: 1px 6px; font-size: 12px; color: #165dff; }
.rw-tip b { color: #1d2129; }

/* 细滚动条（年轻化） */
.traffic-list::-webkit-scrollbar, .code::-webkit-scrollbar, .detail-tabs .el-tabs__content::-webkit-scrollbar,
.mitm-right::-webkit-scrollbar, .mitm-errors::-webkit-scrollbar { width: 8px; height: 8px; }
.traffic-list::-webkit-scrollbar-thumb, .code::-webkit-scrollbar-thumb,
.detail-tabs .el-tabs__content::-webkit-scrollbar-thumb, .mitm-right::-webkit-scrollbar-thumb,
.mitm-errors::-webkit-scrollbar-thumb { background: #d6dbe3; border-radius: 8px; }
.traffic-list::-webkit-scrollbar-thumb:hover, .code::-webkit-scrollbar-thumb:hover,
.detail-tabs .el-tabs__content::-webkit-scrollbar-thumb:hover, .mitm-right::-webkit-scrollbar-thumb:hover { background: #c0c6d0; }
.traffic-list::-webkit-scrollbar-track, .code::-webkit-scrollbar-track,
.detail-tabs .el-tabs__content::-webkit-scrollbar-track, .mitm-right::-webkit-scrollbar-track { background: transparent; }
</style>
