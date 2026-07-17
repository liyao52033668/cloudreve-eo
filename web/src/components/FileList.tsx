import { Table, Button, Dropdown, Modal, Input, message, Space } from 'antd'
import { FolderOutlined, FileOutlined, DownloadOutlined, DeleteOutlined, EditOutlined, MoreOutlined, ShareAltOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import type { FileItem } from '../api/files'
import { deleteFile, renameFile, getDownloadURL } from '../api/files'
import { useState } from 'react'
import ShareModal from './ShareModal'

interface Props {
  files: FileItem[]
  onRefresh: () => void
  onOpenDir: (dirId: number) => void
}

export default function FileList({ files, onRefresh, onOpenDir }: Props) {
  const [renameModal, setRenameModal] = useState<{ visible: boolean; file?: FileItem }>({ visible: false })
  const [newName, setNewName] = useState('')
  const [shareFile, setShareFile] = useState<FileItem | null>(null)

  const formatSize = (bytes: number) => {
    if (bytes === 0) return '-'
    const units = ['B', 'KB', 'MB', 'GB']
    let i = 0
    let size = bytes
    while (size >= 1024 && i < units.length - 1) { size /= 1024; i++ }
    return `${size.toFixed(1)} ${units[i]}`
  }

  const handleDownload = async (file: FileItem) => {
    try {
      const res = await getDownloadURL(file.id)
      window.open(res.data.download_url, '_blank')
    } catch {
      message.error('获取下载链接失败')
    }
  }

  const handleDelete = (file: FileItem) => {
    Modal.confirm({
      title: '确认删除',
      content: `确定删除 "${file.name}" 吗？`,
      onOk: async () => {
        try {
          await deleteFile(file.id)
          message.success('删除成功')
          onRefresh()
        } catch (err: any) {
          message.error(err.response?.data?.error || '删除失败')
        }
      },
    })
  }

  const handleRename = async () => {
    if (!renameModal.file || !newName) return
    try {
      await renameFile(renameModal.file.id, newName)
      message.success('重命名成功')
      setRenameModal({ visible: false })
      onRefresh()
    } catch (err: any) {
      message.error(err.response?.data?.error || '重命名失败')
    }
  }

  const columns: ColumnsType<FileItem> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, record) => (
        <Space>
          {record.is_dir ? <FolderOutlined style={{ color: '#faad14' }} /> : <FileOutlined />}
          <a onClick={() => record.is_dir && onOpenDir(record.id)}>{name}</a>
        </Space>
      ),
    },
    { title: '大小', dataIndex: 'size', width: 120, render: formatSize },
    { title: '修改时间', dataIndex: 'updated_at', width: 180, render: (v: string) => new Date(v).toLocaleString() },
    {
      title: '操作', width: 120,
      render: (_, record) => (
        <Dropdown menu={{
          items: [
            ...(!record.is_dir ? [{ key: 'download', label: '下载', icon: <DownloadOutlined /> }] : []),
            ...(!record.is_dir ? [{ key: 'share', label: '分享', icon: <ShareAltOutlined /> }] : []),
            { key: 'rename', label: '重命名', icon: <EditOutlined /> },
            { key: 'delete', label: '删除', icon: <DeleteOutlined />, danger: true },
          ],
          onClick: ({ key }) => {
            if (key === 'download') handleDownload(record)
            else if (key === 'share') { setShareFile(record) }
            else if (key === 'rename') { setRenameModal({ visible: true, file: record }); setNewName(record.name) }
            else if (key === 'delete') handleDelete(record)
          },
        }}>
          <Button type="text" icon={<MoreOutlined />} />
        </Dropdown>
      ),
    },
  ]

  return (
    <>
      <Table columns={columns} dataSource={files} rowKey="id" pagination={false} />
      <Modal title="重命名" open={renameModal.visible} onOk={handleRename} onCancel={() => setRenameModal({ visible: false })}>
        <Input value={newName} onChange={(e) => setNewName(e.target.value)} />
      </Modal>
      <ShareModal
        open={!!shareFile}
        fileId={shareFile?.id ?? null}
        onClose={() => setShareFile(null)}
      />
    </>
  )
}
