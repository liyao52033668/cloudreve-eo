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
  Switch,
  Tag,
  Empty,
  Popconfirm,
  Typography,
  Row,
  Col,
} from 'antd'
import {
  ArrowLeftOutlined,
  LogoutOutlined,
  PlusOutlined,
  ReloadOutlined,
  EditOutlined,
  DeleteOutlined,
  StarOutlined,
  StarFilled,
  CloudServerOutlined,
} from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import {
  listAdminPolicies,
  getAdminPolicy,
  createPolicy,
  updatePolicy,
  deletePolicy,
  setDefaultPolicy,
  type StoragePolicyAdmin,
  type PolicyForm,
} from '../api/policies'
import { getProfile } from '../api/user'

const { Header, Content } = Layout
const { Text, Paragraph } = Typography

const GiB = 1024 * 1024 * 1024

const emptyForm: PolicyForm = {
  name: '',
  endpoint: '',
  region: 'us-east-1',
  bucket: '',
  access_key: '',
  secret_key: '',
  is_default: false,
  default_quota: 0,
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

export default function StoragePolicies() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [policies, setPolicies] = useState<StoragePolicyAdmin[]>([])
  const [modalOpen, setModalOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm<PolicyForm & { default_quota_gib?: number | null }>()

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
      const res = await listAdminPolicies()
      setPolicies(res.data.policies || [])
    } catch (err: any) {
      if (err.response?.status === 403) {
        message.error('需要管理员权限')
        navigate('/')
      } else {
        message.error(err.response?.data?.error || '加载失败')
      }
    } finally {
      setLoading(false)
    }
  }, [ensureAdmin, navigate])

  useEffect(() => {
    load()
  }, [load])

  const openCreate = () => {
    setEditingId(null)
    form.setFieldsValue({
      ...emptyForm,
      is_default: policies.length === 0,
      default_quota_gib: 0,
    })
    setModalOpen(true)
  }

  const openEdit = async (id: number) => {
    try {
      const res = await getAdminPolicy(id)
      const p = res.data.policy
      setEditingId(id)
      form.setFieldsValue({
        name: p.name,
        endpoint: p.endpoint,
        region: p.region || 'us-east-1',
        bucket: p.bucket,
        access_key: p.access_key,
        secret_key: '', // 留空表示不修改
        is_default: p.is_default,
        default_quota_gib: (p.default_quota || 0) / GiB,
      })
      setModalOpen(true)
    } catch (err: any) {
      message.error(err.response?.data?.error || '加载策略失败')
    }
  }

  const handleSave = async () => {
    try {
      const values = await form.validateFields()
      setSaving(true)
      const gib = Number(values.default_quota_gib ?? 0)
      if (Number.isNaN(gib) || gib < 0) {
        message.error('默认配额不能为负数')
        return
      }
      const payload: PolicyForm = {
        name: values.name,
        endpoint: values.endpoint,
        region: values.region,
        bucket: values.bucket,
        access_key: values.access_key,
        secret_key: values.secret_key || '',
        is_default: !!values.is_default,
        default_quota: Math.round(gib * GiB),
      }
      if (editingId == null) {
        if (!payload.secret_key) {
          message.error('新建时 Secret Key 不能为空')
          return
        }
        await createPolicy(payload)
        message.success('已添加存储策略')
      } else {
        await updatePolicy(editingId, payload)
        message.success('已更新存储策略')
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
      await deletePolicy(id)
      message.success('已删除')
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || '删除失败')
    }
  }

  const handleSetDefault = async (id: number) => {
    try {
      await setDefaultPolicy(id)
      message.success('已设为默认策略')
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || '设置失败')
    }
  }

  const handleLogout = () => {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/login')
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: '#001529' }}>
        <Space>
          <Button type="text" icon={<ArrowLeftOutlined />} style={{ color: '#fff' }} onClick={() => navigate('/')}>
            返回
          </Button>
          <span style={{ color: '#fff', fontSize: 18 }}>存储策略</span>
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
            添加存储策略
          </Button>
        </Space>

        <Paragraph type="secondary" style={{ marginBottom: 16 }}>
          在此添加多套互相独立的 S3 兼容存储（腾讯云 COS、阿里云 OSS、MinIO、Cloudflare R2 等）。每套使用各自凭证、Bucket
          与用户默认配额；上传时可任选其一。配置保存在数据库，修改后立即生效，无需环境变量与重启。
        </Paragraph>

        {policies.length === 0 && !loading ? (
          <Card>
            <Empty description="尚未配置存储策略">
              <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                添加第一个策略
              </Button>
            </Empty>
          </Card>
        ) : (
          <Row gutter={[16, 16]}>
            {policies.map((p) => (
              <Col key={p.id} xs={24} sm={12} lg={8}>
                <Card
                  loading={loading}
                  hoverable
                  actions={[
                    <Button
                      key="default"
                      type="link"
                      icon={p.is_default ? <StarFilled /> : <StarOutlined />}
                      disabled={p.is_default}
                      onClick={() => handleSetDefault(p.id)}
                    >
                      {p.is_default ? '默认' : '设为默认'}
                    </Button>,
                    <Button key="edit" type="link" icon={<EditOutlined />} onClick={() => openEdit(p.id)}>
                      编辑
                    </Button>,
                    <Popconfirm
                      key="del"
                      title="确认删除该策略？"
                      description="已上传到该策略的文件记录不会自动迁移。"
                      onConfirm={() => handleDelete(p.id)}
                    >
                      <Button type="link" danger icon={<DeleteOutlined />}>
                        删除
                      </Button>
                    </Popconfirm>,
                  ]}
                >
                  <Card.Meta
                    avatar={<CloudServerOutlined style={{ fontSize: 28, color: '#1677ff' }} />}
                    title={
                      <Space>
                        <span>{p.name}</span>
                        {p.is_default && <Tag color="blue">默认</Tag>}
                        <Tag>S3 兼容</Tag>
                      </Space>
                    }
                    description={
                      <div style={{ marginTop: 8 }}>
                        <div>
                          <Text type="secondary">Bucket：</Text>
                          {p.bucket}
                        </div>
                        <div style={{ wordBreak: 'break-all' }}>
                          <Text type="secondary">Endpoint：</Text>
                          {p.endpoint}
                        </div>
                        <div>
                          <Text type="secondary">Region：</Text>
                          {p.region || '—'}
                        </div>
                        <div>
                          <Text type="secondary">Access Key：</Text>
                          {p.access_key ? `${p.access_key.slice(0, 4)}••••` : '—'}
                        </div>
                        <div>
                          <Text type="secondary">每用户配额：</Text>
                          {formatBytes(p.default_quota || 0)}
                        </div>
                      </div>
                    }
                  />
                </Card>
              </Col>
            ))}
            <Col xs={24} sm={12} lg={8}>
              <Card
                hoverable
                style={{
                  height: '100%',
                  minHeight: 180,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  borderStyle: 'dashed',
                }}
                onClick={openCreate}
              >
                <div style={{ textAlign: 'center', color: '#999' }}>
                  <PlusOutlined style={{ fontSize: 28 }} />
                  <div style={{ marginTop: 8 }}>添加存储策略</div>
                </div>
              </Card>
            </Col>
          </Row>
        )}
      </Content>

      <Modal
        title={editingId == null ? '添加存储策略' : '编辑存储策略'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={handleSave}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        width={560}
        destroyOnClose
      >
        <Form form={form} layout="vertical" initialValues={{ ...emptyForm, default_quota_gib: 0 }}>
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: '请输入策略名称' }]}
            extra="展示名，也用于写入文件的 storage_policy 字段"
          >
            <Input placeholder="例如 oss、minio、cos" disabled={editingId != null} />
          </Form.Item>
          <Form.Item
            name="bucket"
            label="Bucket 名称"
            rules={[{ required: true, message: '请输入 Bucket' }]}
          >
            <Input placeholder="your-bucket" />
          </Form.Item>
          <Form.Item
            name="endpoint"
            label="Endpoint"
            rules={[{ required: true, message: '请输入 Endpoint' }]}
            extra="S3 API 端点，如 https://oss-cn-shanghai.aliyuncs.com 或 https://cos.ap-guangzhou.myqcloud.com"
          >
            <Input placeholder="https://..." />
          </Form.Item>
          <Form.Item name="region" label="Region" extra="部分服务商必填，默认 us-east-1">
            <Input placeholder="us-east-1" />
          </Form.Item>
          <Form.Item
            name="access_key"
            label="Access Key"
            rules={[{ required: true, message: '请输入 Access Key' }]}
          >
            <Input placeholder="Access Key ID" autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="secret_key"
            label="Secret Key"
            rules={editingId == null ? [{ required: true, message: '请输入 Secret Key' }] : []}
            extra={editingId != null ? '留空表示不修改原密钥' : undefined}
          >
            <Input.Password placeholder={editingId != null ? '留空则不修改' : 'Secret Access Key'} autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="default_quota_gib"
            label="每用户默认配额 (GiB)"
            extra="仅作用于本存储策略。0 表示未配置/不可用，用户在该策略下无法上传。"
            rules={[
              {
                validator: async (_, v) => {
                  if (v === null || v === undefined || v === '') return
                  if (Number(v) < 0) throw new Error('不能为负数')
                },
              },
            ]}
          >
            <InputNumber min={0} step={1} style={{ width: '100%' }} placeholder="0" />
          </Form.Item>
          <Form.Item name="is_default" label="设为默认策略" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </Layout>
  )
}
