<template>
  <div class="clip-overlay" @click.self="onClose">
    <div class="clip-panel" :class="{ 'clip-panel--dark': store.ui.dark }">
      <div class="clip-head">
        <div class="clip-search">
          <input
            ref="searchInput"
            v-model="keyword"
            type="text"
            :placeholder="t('clip.search')"
            @keydown="onSearchKey"
          />
        </div>
        <div class="clip-actions">
          <button class="clip-btn clip-btn--ghost" @click="onClear" :title="t('clip.clear')">
            {{ t('clip.clear') }}
          </button>
          <button class="clip-btn clip-btn--ghost" @click="onClose" :title="t('clip.close')">
            {{ t('clip.close') }}
          </button>
        </div>
      </div>

      <div class="clip-list" ref="listRef" @keydown="onListKey" tabindex="-1">
        <div
          v-for="(item, idx) in filtered"
          :key="item.id"
          class="clip-item"
          :class="{ 'clip-item--active': idx === activeIndex }"
          @mouseenter="activeIndex = idx"
          @click="onCopy(item)"
        >
          <div class="clip-thumb">
            <img v-if="item.type === 'image'" :src="thumbOf(item)" class="clip-thumb-img" alt="" />
            <div v-else class="clip-thumb-text">T</div>
          </div>
          <div class="clip-body">
            <div v-if="item.type === 'image'" class="clip-title">
              {{ t('clip.image') }} · {{ item.width }}×{{ item.height }}
            </div>
            <div v-else class="clip-text">{{ previewText(item.text) }}</div>
            <div class="clip-meta">{{ item.time }}</div>
          </div>
          <div class="clip-item-actions">
            <button class="clip-mini" @click.stop="onCopy(item)" :title="t('clip.copy')">
              {{ t('clip.copy') }}
            </button>
            <button class="clip-mini clip-mini--danger" @click.stop="onDelete(item)" :title="t('clip.delete')">
              ✕
            </button>
          </div>
        </div>

        <div v-if="filtered.length === 0" class="clip-empty">
          {{ keyword ? t('clip.noMatch') : t('clip.empty') }}
        </div>
      </div>

      <div class="clip-foot">
        <span>↑↓ {{ t('clip.nav') }}</span>
        <span>Enter {{ t('clip.copy') }}</span>
        <span>Del {{ t('clip.delete') }}</span>
        <span>Esc {{ t('clip.close') }}</span>
      </div>
    </div>
  </div>
</template>

<script>
import { ref, computed, watch, nextTick, onMounted } from 'vue'
import { store } from '../../store'
import { copyClipItem, removeClip, clearClipHistory, clipImageURL } from '../../store'

export default {
  name: 'ClipboardHistory',
  emits: ['close'],
  setup(props, { emit }) {
    const store = useStore()
    const keyword = ref('')
    const activeIndex = ref(0)
    const searchInput = ref(null)
    const listRef = ref(null)
    const thumbCache = ref({})

    const filtered = computed(() => {
      const kw = keyword.value.trim().toLowerCase()
      const list = store.data.clipboard.history || []
      if (!kw) return list
      return list.filter(it => {
        if (it.type === 'image') return false
        return (it.text || '').toLowerCase().includes(kw)
      })
    })

    function previewText(t) {
      if (!t) return ''
      const one = t.replace(/\s+/g, ' ').trim()
      return one.length > 200 ? one.slice(0, 200) + '…' : one
    }

    function thumbOf(item) {
      if (thumbCache.value[item.id]) return thumbCache.value[item.id]
      clipImageURL(item).then(url => {
        if (url) thumbCache.value[item.id] = url
      })
      return ''
    }

    function t(key) {
      const dict = {
        'zh-CN': {
          'clip.search': '搜索剪贴板…',
          'clip.clear': '清空',
          'clip.close': '关闭',
          'clip.copy': '复制',
          'clip.delete': '删除',
          'clip.image': '图片',
          'clip.empty': '暂无剪贴板历史',
          'clip.noMatch': '没有匹配的记录',
          'clip.nav': '选择',
        },
        'en': {
          'clip.search': 'Search clipboard…',
          'clip.clear': 'Clear',
          'clip.close': 'Close',
          'clip.copy': 'Copy',
          'clip.delete': 'Delete',
          'clip.image': 'Image',
          'clip.empty': 'No clipboard history',
          'clip.noMatch': 'No matching records',
          'clip.nav': 'navigate',
        },
      }
      const lang = store.data.settings.lang || 'zh-CN'
      return (dict[lang] || dict['en'])[key] || key
    }

    function ensureActive() {
      if (activeIndex.value >= filtered.value.length) activeIndex.value = Math.max(0, filtered.value.length - 1)
    }

    function onCopy(item) {
      copyClipItem(item.id)
      emit('close')
    }

    function onDelete(item) {
      const i = filtered.value.indexOf(item)
      removeClip(item.id)
      if (thumbCache.value[item.id]) delete thumbCache.value[item.id]
      nextTick(() => ensureActive())
      void i
    }

    function onClear() {
      clearClipHistory()
      thumbCache.value = {}
      activeIndex.value = 0
    }

    function onClose() {
      emit('close')
    }

    function onSearchKey(e) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        activeIndex.value = Math.min(filtered.value.length - 1, activeIndex.value + 1)
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        activeIndex.value = Math.max(0, activeIndex.value - 1)
      } else if (e.key === 'Enter') {
        e.preventDefault()
        const it = filtered.value[activeIndex.value]
        if (it) onCopy(it)
      } else if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
    }

    function onListKey(e) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        activeIndex.value = Math.min(filtered.value.length - 1, activeIndex.value + 1)
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        activeIndex.value = Math.max(0, activeIndex.value - 1)
      } else if (e.key === 'Enter') {
        e.preventDefault()
        const it = filtered.value[activeIndex.value]
        if (it) onCopy(it)
      } else if (e.key === 'Delete' || e.key === 'Backspace') {
        e.preventDefault()
        const it = filtered.value[activeIndex.value]
        if (it) onDelete(it)
      } else if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
      scrollActiveIntoView()
    }

    function scrollActiveIntoView() {
      nextTick(() => {
        const el = listRef.value
        if (!el) return
        const active = el.querySelector('.clip-item--active')
        if (active) active.scrollIntoView({ block: 'nearest' })
      })
    }

    watch(filtered, () => ensureActive())
    watch(keyword, () => { activeIndex.value = 0 })

    onMounted(() => {
      nextTick(() => {
        if (searchInput.value) searchInput.value.focus()
      })
    })

    return {
      store, keyword, activeIndex, searchInput, listRef, filtered,
      previewText, thumbOf, t, onCopy, onDelete, onClear, onClose, onSearchKey, onListKey,
    }
  },
}
</script>

<style scoped>
.clip-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 9999;
}
.clip-panel {
  width: min(760px, 92vw);
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  background: #fff;
  border-radius: 12px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.35);
  overflow: hidden;
  color: #1f2329;
}
.clip-panel--dark {
  background: #1e1e1e;
  color: #e6e6e6;
}
.clip-head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}
.clip-panel--dark .clip-head { border-color: rgba(255, 255, 255, 0.08); }
.clip-search { flex: 1; }
.clip-search input {
  width: 100%;
  height: 36px;
  padding: 0 12px;
  border: 1px solid #dcdfe6;
  border-radius: 8px;
  outline: none;
  font-size: 14px;
  background: #f7f8fa;
  color: inherit;
}
.clip-panel--dark .clip-search input { background: #2a2a2a; border-color: #3a3a3a; }
.clip-search input:focus { border-color: #409eff; }
.clip-actions { display: flex; gap: 8px; }
.clip-btn {
  height: 32px;
  padding: 0 12px;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  font-size: 13px;
}
.clip-btn--ghost {
  background: #f0f2f5;
  color: #4a4a4a;
}
.clip-panel--dark .clip-btn--ghost { background: #333; color: #ccc; }
.clip-btn--ghost:hover { background: #e4e7ed; }
.clip-panel--dark .clip-btn--ghost:hover { background: #3d3d3d; }
.clip-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
  outline: none;
}
.clip-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
}
.clip-item--active { background: #ecf5ff; }
.clip-panel--dark .clip-item--active { background: #2b3a4a; }
.clip-thumb {
  width: 48px;
  height: 48px;
  flex: 0 0 48px;
  border-radius: 6px;
  overflow: hidden;
  background: #f0f0f0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.clip-panel--dark .clip-thumb { background: #2a2a2a; }
.clip-thumb-img { width: 100%; height: 100%; object-fit: cover; }
.clip-thumb-text {
  font-weight: 700;
  color: #909399;
}
.clip-body { flex: 1; min-width: 0; }
.clip-text {
  font-size: 13px;
  line-height: 1.4;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: inherit;
}
.clip-title { font-size: 13px; color: #606266; }
.clip-panel--dark .clip-title { color: #aaa; }
.clip-meta { font-size: 11px; color: #a0a0a0; margin-top: 2px; }
.clip-item-actions { display: flex; gap: 6px; flex: 0 0 auto; }
.clip-mini {
  height: 26px;
  padding: 0 10px;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  background: #fff;
  color: #409eff;
  cursor: pointer;
  font-size: 12px;
}
.clip-panel--dark .clip-mini { background: #2a2a2a; border-color: #3a3a3a; color: #79bbff; }
.clip-mini:hover { background: #ecf5ff; }
.clip-mini--danger { color: #f56c6c; }
.clip-mini--danger:hover { background: #fef0f0; }
.clip-empty {
  text-align: center;
  color: #a0a0a0;
  padding: 40px 0;
  font-size: 14px;
}
.clip-foot {
  display: flex;
  gap: 16px;
  padding: 8px 14px;
  border-top: 1px solid rgba(0, 0, 0, 0.08);
  font-size: 12px;
  color: #909399;
}
.clip-panel--dark .clip-foot { border-color: rgba(255, 255, 255, 0.08); color: #888; }
</style>
