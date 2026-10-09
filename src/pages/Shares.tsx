import { useState, useCallback, useEffect } from 'react'
import {
  Layout,
  Card,
  Button,
  Space,
  Tag,
  Empty,
  message,
  Popconfirm,
  Modal,
  List,
  Typography,
  Tooltip,
  Row,
  Col,
} from 'antd'
import {
  DeleteOutlined,
  PlusOutlined,
  MinusCircleOutlined,
  ReloadOutlined,
  LinkOutlined,
  EyeOutlined,
  ClockCircleOutlined,
  FileOutlined,
  FolderOutlined,
  CopyOutlined,
} from '@ant-design/icons'
import dayjs from 'dayjs'
import AppHeader from '../components/AppHeader'
import {
  listMyShares,
  deleteShare,
  addShareFiles,
  removeShareFiles,
  type ShareWithFiles,
} from '../api/shares'
import { listFiles, type FileItem } from '../api/files'
import { copyText } from '../utils/clipboard'

const { Content } = Layout
const { Text } = Typography

/** 格式化字节大小 */
function formatBytes(n: number): string {
  if (!n || n <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let i = 0
  let size = n
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024
    i++
  }
  return i === 0 ? `${size} B` : `${size.toFixed(2)} ${units[i]}`
}

/** 分享是否已过期 */
function isExpired(expireAt: string | null): boolean {
  return !!expireAt && dayjs(expireAt).isBefore(dayjs())
}

export default function Shares() {
  const [loading, setLoading] = useState(false)
  const [shares, setShares] = useState<ShareWithFiles[]>([])

  // 添加文件弹窗
  const [addModalOpen, setAddModalOpen] = useState(false)
  const [addTargetShare, setAddTargetShare] = useState<ShareWithFiles | null>(null)
  const [addFiles, setAddFiles] = useState<FileItem[]>([])
  const [addLoading, setAddLoading] = useState(false)
  const [addSelected, setAddSelected] = useState<Set<number>>(new Set())
  const [addBreadcrumbs, setAddBreadcrumbs] = useState<{ id: number; name: string }[]>([
    { id: 0, name: '根目录' },
  ])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await listMyShares()
      setShares(res.data.shares || [])
    } catch {
      message.error('加载分享列表失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const handleDelete = async (id: number) => {
    try {
      await deleteShare(id)
      message.success('已取消分享')
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || '操作失败')
    }
  }

  const handleCopyLink = async (code: string) => {
    const link = `${window.location.origin}/share/${code}`
    try {
      await copyText(link)
      message.success('链接已复制')
    } catch {
      message.error('复制失败')
    }
  }

  // ─── 添加文件弹窗 ───

  const openAddModal = (share: ShareWithFiles) => {
    setAddTargetShare(share)
    setAddSelected(new Set())
    setAddBreadcrumbs([{ id: 0, name: '根目录' }])
    setAddModalOpen(true)
    loadAddFiles(0)
  }

  const loadAddFiles = async (parentId: number) => {
    setAddLoading(true)
    try {
      const res = await listFiles(parentId)
      setAddFiles(res.data.files || [])
    } catch {
      message.error('加载文件列表失败')
    } finally {
      setAddLoading(false)
    }
  }

  const navigateToAddDir = (dir: FileItem) => {
    setAddBreadcrumbs((prev) => [...prev, { id: dir.id, name: dir.name }])
    setAddSelected(new Set())
    loadAddFiles(dir.id)
  }

  const navigateToAddBreadcrumb = (index: number) => {
    const target = addBreadcrumbs[index]
    setAddBreadcrumbs((prev) => prev.slice(0, index + 1))
    setAddSelected(new Set())
    loadAddFiles(target.id)
  }

  const toggleAddFile = (id: number) => {
    setAddSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const handleAddFiles = async () => {
    if (!addTargetShare || addSelected.size === 0) return
    setAddLoading(true)
    try {
      await addShareFiles(addTargetShare.id, Array.from(addSelected))
      message.success('已添加文件')
      setAddModalOpen(false)
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || '添加失败')
    } finally {
      setAddLoading(false)
    }
  }

  // ─── 移除文件 ───

  const handleRemoveFile = async (share: ShareWithFiles, fileId: number) => {
    try {
      await removeShareFiles(share.id, [fileId])
      message.success('已移除文件')
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || '移除失败')
    }
  }

  // ─── 渲染 ───

  const shareLink = (code: string) => `${window.location.origin}/share/${code}`

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <AppHeader title="我的分享" />
      <Content className="page-content" style={{ padding: 24, maxWidth: 1100, margin: '0 auto', width: '100%' }}>
        <Space style={{ marginBottom: 16, flexWrap: 'wrap', width: '100%' }}>
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
            刷新
          </Button>
        </Space>

        {shares.length === 0 && !loading ? (
          <Card>
            <Empty description="还没有分享过任何文件">
              <Text type="secondary">在「我的文件」中选择文件后即可创建分享</Text>
            </Empty>
          </Card>
        ) : (
          <Row gutter={[16, 16]}>
            {shares.map((share) => {
              const expired = isExpired(share.expire_at)
              return (
                <Col key={share.id} xs={24} lg={12}>
                  <Card
                    hoverable
                    styles={{ body: { padding: '16px 20px' } }}
                    actions={[
                      <Tooltip key="copy" title="复制分享链接">
                        <Button
                          type="link"
                          icon={<CopyOutlined />}
                          onClick={() => handleCopyLink(share.code)}
                        >
                          复制链接
                        </Button>
                      </Tooltip>,
                      <Tooltip key="add" title="添加文件到此分享">
                        <Button
                          type="link"
                          icon={<PlusOutlined />}
                          onClick={() => openAddModal(share)}
                        >
                          添加文件
                        </Button>
                      </Tooltip>,
                      <Popconfirm
                        key="del"
                        title="确认取消此分享？"
                        description="取消后他人将无法再通过此链接访问"
                        onConfirm={() => handleDelete(share.id)}
                      >
                        <Button type="link" danger icon={<DeleteOutlined />}>
                          取消分享
                        </Button>
                      </Popconfirm>,
                    ]}
                  >
                    <div style={{ marginBottom: 12 }}>
                      <Space>
                        <LinkOutlined style={{ color: '#1677ff' }} />
                        <Text copyable={{ text: shareLink(share.code) }} style={{ fontSize: 13, wordBreak: 'break-all' }}>
                          {shareLink(share.code)}
                        </Text>
                      </Space>
                    </div>

                    <Space style={{ marginBottom: 8, flexWrap: 'wrap' }}>
                      <Tag icon={<EyeOutlined />} color="processing">{share.views} 次浏览</Tag>
                      <Tag icon={<ClockCircleOutlined />}>
                        {expired ? '已过期' : share.expire_at
                          ? `有效期至 ${dayjs(share.expire_at).format('YYYY-MM-DD HH:mm')}`
                          : '永久有效'}
                      </Tag>
                      <Tag>{dayjs(share.created_at).format('YYYY-MM-DD HH:mm')}</Tag>
                    </Space>

                    <div style={{ marginTop: 8 }}>
                      <Text type="secondary" style={{ fontSize: 13 }}>
                        分享文件（{share.files.length}）：
                      </Text>
                      <List
                        size="small"
                        dataSource={share.files}
                        renderItem={(file) => (
                          <List.Item
                            style={{ padding: '6px 0', border: 'none' }}
                            actions={[
                              <Popconfirm
                                key="rm"
                                title="确认移除此文件？"
                                onConfirm={() => handleRemoveFile(share, file.id)}
                              >
                                <Button
                                  type="text"
                                  size="small"
                                  danger
                                  icon={<MinusCircleOutlined />}
                                />
                              </Popconfirm>,
                            ]}
                          >
                            <List.Item.Meta
                              avatar={
                                file.is_dir
                                  ? <FolderOutlined style={{ fontSize: 16, color: '#faad14' }} />
                                  : <FileOutlined style={{ fontSize: 16, color: '#8c8c8c' }} />
                              }
                              title={
                                <Text ellipsis style={{ maxWidth: 200, fontSize: 13 }}>
                                  {file.name}
                                </Text>
                              }
                              description={
                                <Text type="secondary" style={{ fontSize: 12 }}>
                                  {file.is_dir ? '文件夹' : formatBytes(file.size)}
                                </Text>
                              }
                            />
                          </List.Item>
                        )}
                      />
                    </div>
                  </Card>
                </Col>
              )
            })}
          </Row>
        )}
      </Content>

      {/* 添加文件弹窗 */}
      <Modal
        title="添加文件到分享"
        open={addModalOpen}
        onCancel={() => setAddModalOpen(false)}
        onOk={handleAddFiles}
        okText="添加选中文件"
        cancelText="取消"
        confirmLoading={addLoading}
        okButtonProps={{ disabled: addSelected.size === 0 }}
        width="min(560px, 94vw)"
        destroyOnHidden
      >
        {/* 面包屑导航 */}
        <Space style={{ marginBottom: 12, flexWrap: 'wrap' }}>
          {addBreadcrumbs.map((crumb, index) => (
            <span key={crumb.id}>
              {index > 0 && <Text type="secondary"> / </Text>}
              {index === addBreadcrumbs.length - 1 ? (
                <Text strong>{crumb.name}</Text>
              ) : (
                <Button type="link" size="small" onClick={() => navigateToAddBreadcrumb(index)}>
                  {crumb.name}
                </Button>
              )}
            </span>
          ))}
        </Space>

        <List
          size="small"
          loading={addLoading}
          dataSource={addFiles}
          locale={{ emptyText: '此目录为空' }}
          style={{ maxHeight: 360, overflowY: 'auto' }}
          renderItem={(file) => {
            const alreadyInShare = addTargetShare?.files.some((f) => f.id === file.id) ?? false
            const isSelected = addSelected.has(file.id)
            return (
              <List.Item
                style={{
                  cursor: file.is_dir || (!alreadyInShare && !file.is_dir) ? 'pointer' : 'not-allowed',
                  opacity: alreadyInShare ? 0.5 : 1,
                  padding: '8px 4px',
                }}
                onClick={() => {
                  if (file.is_dir) {
                    navigateToAddDir(file)
                  } else if (!alreadyInShare) {
                    toggleAddFile(file.id)
                  }
                }}
              >
                <List.Item.Meta
                  avatar={
                    file.is_dir
                      ? <FolderOutlined style={{ fontSize: 18, color: '#faad14' }} />
                      : <FileOutlined style={{ fontSize: 18, color: isSelected ? '#1677ff' : '#8c8c8c' }} />
                  }
                  title={
                    <Space>
                      <Text ellipsis style={{ maxWidth: 280 }}>{file.name}</Text>
                      {alreadyInShare && <Tag color="default">已在分享中</Tag>}
                    </Space>
                  }
                  description={
                    <Text type="secondary" style={{ fontSize: 12 }}>
                      {file.is_dir ? '文件夹' : formatBytes(file.size)}
                    </Text>
                  }
                />
                {!file.is_dir && !alreadyInShare && (
                  <input
                    type="checkbox"
                    checked={isSelected}
                    onChange={() => toggleAddFile(file.id)}
                    style={{ marginLeft: 8 }}
                  />
                )}
              </List.Item>
            )
          }}
        />
      </Modal>
    </Layout>
  )
}
