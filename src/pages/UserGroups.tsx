import { useCallback, useEffect, useState } from 'react'
import {
  Layout,
  Card,
  Button,
  Space,
  message,
  Modal,
  Form,
  Input,
  InputNumber,
  Select,
  Switch,
  Tag,
  Empty,
  Popconfirm,
  Typography,
  Row,
  Col,
} from 'antd'
import {
  PlusOutlined,
  ReloadOutlined,
  EditOutlined,
  DeleteOutlined,
  StarOutlined,
  StarFilled,
  TeamOutlined,
} from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import {
  listAdminGroups,
  getAdminGroup,
  createGroup,
  updateGroup,
  deleteGroup,
  setDefaultGroup,
  type GroupView,
  type GroupForm,
} from '../api/groups'
import {
  listAdminPolicies,
  type StoragePolicyAdmin,
} from '../api/policies'
import { getProfile } from '../api/user'
import AppHeader from '../components/AppHeader'

const { Content } = Layout
const { Text, Paragraph } = Typography

const GiB = 1024 * 1024 * 1024

const emptyForm: GroupForm = {
  name: '',
  storage_policies: [],
  max_storage: 0,
  is_default: false,
}

function formatBytes(n: number): string {
  if (!n || n <= 0) return '0（未配置）'
  if (n >= GiB) {
    const g = n / GiB
    return Number.isInteger(g) ? `${g} GiB` : `${g.toFixed(2)} GiB`
  }
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(2)} MiB`
  if (n >= 1024) return `${(n / 1024).toFixed(2)} KiB`
  return `${n} B`
}

export default function UserGroups() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [groups, setGroups] = useState<GroupView[]>([])
  const [modalOpen, setModalOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm<GroupForm>()
  const [policies, setPolicies] = useState<StoragePolicyAdmin[]>([])

  const ensureAdmin = useCallback(async () => {
    try {
      const res = await getProfile()
      if (!res.data.user?.is_admin) {
        message.error('需要管理员权限')
        navigate('/')
        return false
      }
      return true
    } catch {
      navigate('/login')
      return false
    }
  }, [navigate])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const ok = await ensureAdmin()
      if (!ok) return
      const res = await listAdminGroups()
      setGroups(res.data.groups || [])
    } catch (err: any) {
      if (err.response?.status === 403) {
        message.error('需要管理员权限')
        navigate('/')
      } else {
        message.error(err.response?.data?.error || '加载用户组失败')
      }
    } finally {
      setLoading(false)
    }
  }, [ensureAdmin, navigate])

  useEffect(() => {
    load()
  }, [load])

  const loadPolicies = useCallback(async () => {
    try {
      const res = await listAdminPolicies()
      setPolicies(res.data.policies || [])
    } catch {
      // ignore
    }
  }, [])

  useEffect(() => {
    loadPolicies()
  }, [loadPolicies])

  const openCreate = () => {
    setEditingId(null)
    form.setFieldsValue({
      ...emptyForm,
      is_default: groups.length === 0,
    })
    setModalOpen(true)
  }

  const openEdit = async (id: number) => {
    try {
      const res = await getAdminGroup(id)
      const g = res.data.group
      setEditingId(id)
      form.setFieldsValue({
        name: g.name,
        storage_policies: g.storage_policies || [g.storage_policy || ''],
        max_storage: (g.max_storage || 0) / GiB,
        is_default: g.is_default,
      })
      setModalOpen(true)
    } catch (err: any) {
      message.error(err.response?.data?.error || '加载用户组失败')
    }
  }

  const handleSave = async () => {
    try {
      const values = await form.validateFields()
      setSaving(true)
      const payload: GroupForm = {
        name: values.name,
        storage_policies: values.storage_policies || [],
        max_storage: Math.round(Number(values.max_storage) * GiB),
        is_default: !!values.is_default,
      }
      if (editingId == null) {
        await createGroup(payload)
        message.success('已添加用户组')
      } else {
        await updateGroup(editingId, payload)
        message.success('已更新用户组')
      }
      setModalOpen(false)
      load()
    } catch (err: any) {
      if (err?.errorFields) return // form validation
      message.error(err.response?.data?.error || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (id: number) => {
    try {
      await deleteGroup(id)
      message.success('已删除')
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || '删除失败')
    }
  }

  const handleSetDefault = async (id: number) => {
    try {
      await setDefaultGroup(id)
      message.success('已设为默认组')
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || '设置失败')
    }
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <AppHeader title="用户组" />
      <Content style={{ padding: 24, maxWidth: 1100, margin: '0 auto', width: '100%' }}>
        <Space style={{ marginBottom: 16 }}>
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            添加用户组
          </Button>
        </Space>

        <Paragraph type="secondary" style={{ marginBottom: 16 }}>
          用户组管理。每个用户属于一个组，上传策略由组决定。
        </Paragraph>

        {groups.length === 0 && !loading ? (
          <Card>
            <Empty description="尚未创建用户组">
              <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                添加第一个用户组
              </Button>
            </Empty>
          </Card>
        ) : (
          <Row gutter={[16, 16]}>
            {groups.map((g) => (
              <Col key={g.id} xs={24} sm={12} lg={8}>
                <Card
                  loading={loading}
                  hoverable
                  actions={[
                    <Button
                      key="default"
                      type="link"
                      icon={g.is_default ? <StarFilled /> : <StarOutlined />}
                      disabled={g.is_default}
                      onClick={() => handleSetDefault(g.id)}
                    >
                      {g.is_default ? '默认' : '设为默认'}
                    </Button>,
                    <Button key="edit" type="link" icon={<EditOutlined />} onClick={() => openEdit(g.id)}>
                      编辑
                    </Button>,
                    <Popconfirm
                      key="del"
                      title="确认删除该用户组？"
                      description="组内用户将并入默认组"
                      onConfirm={() => handleDelete(g.id)}
                    >
                      <Button type="link" danger icon={<DeleteOutlined />}>
                        删除
                      </Button>
                    </Popconfirm>,
                  ]}
                >
                  <Card.Meta
                    avatar={<TeamOutlined style={{ fontSize: 28, color: '#1677ff' }} />}
                    title={
                      <Space>
                        <span>{g.name}</span>
                        {g.is_default && <Tag color="blue">默认</Tag>}
                      </Space>
                    }
                    description={
                      <div style={{ marginTop: 8 }}>
                        <div>
                          <Text type="secondary">存储策略：</Text>
                          {g.storage_policies && g.storage_policies.length > 0
                            ? g.storage_policies.join(', ')
                            : (g.storage_policy || '默认策略')}
                        </div>
                        <div>
                          <Text type="secondary">每用户最大容量：</Text>
                          {g.max_storage > 0 ? formatBytes(g.max_storage) : '沿用策略默认配额'}
                        </div>
                        <div>
                          <Text type="secondary">用户数：</Text>
                          {g.user_count}
                        </div>
                        <div>
                          <Text type="secondary">已用空间：</Text>
                          {formatBytes(g.storage_used)}
                        </div>
                      </div>
                    }
                  />
                </Card>
              </Col>
            ))}
          </Row>
        )}
      </Content>

      <Modal
        title={editingId == null ? '添加用户组' : '编辑用户组'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={handleSave}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        width={560}
        destroyOnClose
      >
        <Form form={form} layout="vertical" initialValues={{ ...emptyForm }}>
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: '请输入组名称' }]}
          >
            <Input placeholder="例如 admin, user" />
          </Form.Item>
          <Form.Item
            name="storage_policies"
            label="存储策略"
            extra="可多选，空表示跟随默认策略"
            rules={[{ required: true, message: '请选择至少一个策略' }]}
          >
            <Select
              mode="multiple"
              options={[
                { value: '', label: '跟随默认策略' },
                ...policies.map((p) => ({ value: p.name, label: p.name })),
              ]}
              placeholder="选择存储策略（支持多选）"
              filterOption={(input, option) =>
                (option?.label ?? '').toLowerCase().includes(input.toLowerCase())
              }
            />
          </Form.Item>
          <Form.Item
            name="max_storage"
            label="每用户最大容量 (GiB)"
            extra="0 表示沿用策略默认配额"
            rules={[
              { required: true, message: '请输入容量' },
              { type: 'number', min: 0, message: '不能为负数' },
            ]}
          >
            <InputNumber min={0} step={1} style={{ width: '100%' }} placeholder="0" />
          </Form.Item>
          <Form.Item name="is_default" label="设为默认组" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </Layout>
  )
}