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
  Switch,
  Tag,
  Empty,
  Popconfirm,
  Typography,
  Row,
  Col,
  Select,
} from 'antd'
import {
  ArrowLeftOutlined,
  LogoutOutlined,
  PlusOutlined,
  ReloadOutlined,
  EditOutlined,
  DeleteOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import {
  listAdminGroups,
  type GroupView,
} from '../api/groups'
import {
  listAdminUsers,
  getAdminUser,
  createUser,
  updateUser,
  deleteUser,
  type AdminUser,
} from '../api/adminUsers'
import { getProfile } from '../api/user'

const { Header, Content } = Layout
const { Text, Paragraph } = Typography

const GiB = 1024 * 1024 * 1024

const emptyForm = {
  username: '',
  password: '',
  group_id: 0,
  is_admin: false,
}

function formatBytes(n: number): string {
  if (!n || n <= 0) return '0'
  if (n >= GiB) {
    const g = n / GiB
    return Number.isInteger(g) ? `${g} GiB` : `${g.toFixed(2)} GiB`
  }
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(2)} MiB`
  if (n >= 1024) return `${(n / 1024).toFixed(2)} KiB`
  return `${n} B`
}

export default function Users() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [users, setUsers] = useState<AdminUser[]>([])
  const [modalOpen, setModalOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm<any>()
  const [groups, setGroups] = useState<GroupView[]>([])

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
      const res = await listAdminUsers()
      setUsers(res.data.users || [])
    } catch (err: any) {
      if (err.response?.status === 403) {
        message.error('需要管理员权限')
        navigate('/')
      } else {
        message.error(err.response?.data?.error || '加载用户失败')
      }
    } finally {
      setLoading(false)
    }
  }, [ensureAdmin, navigate])

  useEffect(() => {
    load()
  }, [load])

  const loadGroups = useCallback(async () => {
    try {
      const res = await listAdminGroups()
      setGroups(res.data.groups || [])
    } catch {
      // ignore
    }
  }, [])

  useEffect(() => {
    loadGroups()
  }, [loadGroups])

  const openCreate = () => {
    setEditingId(null)
    form.setFieldsValue({
      ...emptyForm,
      password: '',
    })
    setModalOpen(true)
  }

  const openEdit = async (id: number) => {
    try {
      const res = await getAdminUser(id)
      const u = res.data.user
      setEditingId(id)
      form.setFieldsValue({
        username: u.username,
        password: '',
        group_id: u.group_id,
        is_admin: u.is_admin,
      })
      setModalOpen(true)
    } catch (err: any) {
      message.error(err.response?.data?.error || '加载用户失败')
    }
  }

  const handleSave = async () => {
    try {
      const values = await form.validateFields()
      setSaving(true)
      const payload: any = {
        username: values.username,
        group_id: values.group_id,
        is_admin: values.is_admin,
      }
      if (values.password) {
        payload.password = values.password
      }
      if (editingId == null) {
        await createUser(payload)
        message.success('已添加用户')
      } else {
        await updateUser(editingId, payload)
        message.success('已更新用户')
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
      await deleteUser(id)
      message.success('已删除')
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || '删除失败')
    }
  }

  const handleLogout = () => {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/login')
  }

  const groupOptions = groups.map((g) => ({
    value: g.id,
    label: g.name,
  }))

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: '#001529' }}>
        <Space>
          <Button type="text" icon={<ArrowLeftOutlined />} style={{ color: '#fff' }} onClick={() => navigate('/')}>
            返回
          </Button>
          <span style={{ color: '#fff', fontSize: 18 }}>用户</span>
        </Space>
        <Button icon={<LogoutOutlined />} type="text" style={{ color: '#fff' }} onClick={handleLogout}>
          退出
        </Button>
      </Header>
      <Content style={{ padding: 24, maxWidth: 1100, margin: '0 auto', width: '100%' }}>
        <Space style={{ marginBottom: 16 }}>
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            添加用户
          </Button>
        </Space>

        <Paragraph type="secondary" style={{ marginBottom: 16 }}>
          用户管理。设置用户组、容量等。
        </Paragraph>

        {users.length === 0 && !loading ? (
          <Card>
            <Empty description="尚未创建用户">
              <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                添加第一个用户
              </Button>
            </Empty>
          </Card>
        ) : (
          <Row gutter={[16, 16]}>
            {users.map((u) => (
              <Col key={u.id} xs={24} sm={12} lg={8}>
                <Card
                  loading={loading}
                  hoverable
                  actions={[
                    <Button key="edit" type="link" icon={<EditOutlined />} onClick={() => openEdit(u.id)}>
                      编辑
                    </Button>,
                    <Popconfirm
                      key="del"
                      title="确认删除该用户？"
                      description="将删除该用户全部文件与存储端对象，不可恢复"
                      onConfirm={() => handleDelete(u.id)}
                    >
                      <Button type="link" danger icon={<DeleteOutlined />}>
                        删除
                      </Button>
                    </Popconfirm>,
                  ]}
                >
                  <Card.Meta
                    avatar={<UserOutlined style={{ fontSize: 28, color: '#1677ff' }} />}
                    title={
                      <Space>
                        <span>{u.username}</span>
                        {u.is_admin && <Tag color="red">管理员</Tag>}
                      </Space>
                    }
                    description={
                      <div style={{ marginTop: 8 }}>
                        <div>
                          <Text type="secondary">所属组：</Text>
                          {u.group_name || '默认组'}
                        </div>
                        <div>
                          <Text type="secondary">已用空间：</Text>
                          {formatBytes(u.storage_used)}
                        </div>
                        <div>
                          <Text type="secondary">创建时间：</Text>
                          {u.created_at}
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
        title={editingId == null ? '添加用户' : '编辑用户'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={handleSave}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        width={560}
        destroyOnClose
      >
        <Form form={form} layout="vertical" initialValues={emptyForm}>
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input placeholder="用户名" />
          </Form.Item>
          <Form.Item
            name="password"
            label={editingId == null ? '密码 (至少6位)' : '新密码 (留空不修改)'}
            rules={editingId == null ? [{ required: true, min: 6, message: '密码至少6位' }] : []}
          >
            <Input.Password placeholder="请输入密码" autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="group_id"
            label="所属用户组"
          >
            <Select
              options={groupOptions}
              placeholder="选择用户组"
            />
          </Form.Item>
          <Form.Item name="is_admin" label="管理员" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </Layout>
  )
}