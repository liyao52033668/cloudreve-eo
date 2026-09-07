import { Layout, Typography, Divider } from 'antd'

const { Content } = Layout
const { Title, Paragraph, Text } = Typography

export default function PrivacyPolicy() {
  return (
    <Layout style={{ minHeight: '100vh', background: '#fff' }}>
      <Content style={{ maxWidth: 800, margin: '0 auto', padding: '48px 24px' }}>
        <Title level={1}>隐私政策</Title>
        <Paragraph type="secondary">生效日期：2026 年 9 月 7 日</Paragraph>

        <Paragraph>
          欢迎访问 xiaoying.org.cn（以下简称"我们"或"本应用"）。我们非常重视用户的隐私保护。本隐私政策旨在向您说明我们如何收集、使用、存储和保护您的个人信息，特别是通过 Google OAuth 服务获取的数据。
        </Paragraph>

        <Divider />

        <Title level={2}>1. 我们收集的信息</Title>
        <Paragraph>
          当您使用 Google 账号登录本应用时，在获得您明确授权的前提下，我们可能会获取以下信息：
        </Paragraph>
        <ul style={{ paddingLeft: 24 }}>
          <li style={{ marginBottom: 8 }}>
            <Text strong>基础个人资料信息：</Text>您的 Google 账号名称、电子邮箱地址以及个人头像。
          </li>
          <li style={{ marginBottom: 8 }}>
            <Text strong>特定 Google 服务数据：</Text>仅限您在使用本应用特定功能时，授权我们访问的 Google 云端硬盘特定文件。
          </li>
        </ul>

        <Title level={2}>2. 我们如何使用您的信息</Title>
        <Paragraph>
          我们严格遵守 Google API 服务用户数据政策。我们收集的信息将仅用于以下用途：
        </Paragraph>
        <ul style={{ paddingLeft: 24 }}>
          <li style={{ marginBottom: 8 }}>
            <Text strong>身份验证：</Text>用于识别您的用户身份，以便您登录并使用本应用的服务。
          </li>
          <li style={{ marginBottom: 8 }}>
            <Text strong>提供核心服务：</Text>仅用于实现本应用内您所触发的具体功能（如管理您选定的文件）。
          </li>
        </ul>
        <Paragraph>
          <Text strong>我们郑重承诺：</Text>我们绝不会将您的 Google 用户数据用于广告投放，也绝不会将这些数据出售、出租或泄露给任何第三方。
        </Paragraph>

        <Title level={2}>3. 数据的存储与安全</Title>
        <Paragraph>
          我们采用行业标准的加密技术和安全措施来保护您的数据，防止未经授权的访问、篡改或泄露。
        </Paragraph>
        <Paragraph>
          您的 Google 授权凭证（如 Access Token）仅在必要的时间段内安全地存储或传输，您随时可以撤销授权。
        </Paragraph>

        <Title level={2}>4. 您的权利与数据撤销</Title>
        <Paragraph>您可以随时控制或删除您的数据：</Paragraph>
        <ul style={{ paddingLeft: 24 }}>
          <li style={{ marginBottom: 8 }}>
            <Text strong>撤销授权：</Text>您可以随时在您的 Google 账号安全设置页面中撤销本应用的访问权限。
          </li>
          <li style={{ marginBottom: 8 }}>
            <Text strong>删除账号：</Text>如果您希望删除在本应用中的所有个人数据，可以通过下方列出的联系方式与我们联系，我们将在收到请求后及时处理。
          </li>
        </ul>

        <Title level={2}>5. 政策更新</Title>
        <Paragraph>
          我们可能会不时更新本隐私政策。任何重大变更都将在本页面上公布，并更新上方的生效日期。
        </Paragraph>

        <Title level={2}>6. 联系我们</Title>
        <Paragraph>
          如果您对本隐私政策或数据处理有任何疑问、意见或建议，请通过以下方式与我们联系：
        </Paragraph>
        <Paragraph>
          <Text strong>电子邮箱：</Text>
          <a href="mailto:liyao52033@gmail.com">liyao52033@gmail.com</a>
        </Paragraph>
      </Content>
    </Layout>
  )
}
