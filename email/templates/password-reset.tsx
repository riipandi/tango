import { Hr } from 'react-email'
import { Alert, Button, CardFooter, CardHeader, Text } from '../components'
import { sharedPreviewProps, sharedTemplateProps } from '../constants'
import { BaseTemplate } from '../layouts'

interface PasswordResetData {
  email: string
  resetLink: string
}

interface PasswordResetProps {
  logoURL: string
  appName: string
  data: PasswordResetData
}

export const PasswordReset = ({ logoURL, appName, data }: PasswordResetProps) => {
  return (
    <BaseTemplate logoURL={logoURL} appName={appName}>
      <CardHeader title='Reset Your Password' />
      <Hr style={{ marginTop: '16px' }} />
      <Text>Hello,</Text>

      <Text>
        You requested to reset your password for <strong>{data.email}</strong>.
      </Text>

      <Text>Click the link below to reset your password:</Text>

      <Button href={data.resetLink}>Reset Password</Button>

      <Text style={{ color: '#5b667e', marginBottom: '1px' }}>
        Or copy and paste this link into your browser:
      </Text>

      <Text style={{ color: '#5b667e', wordBreak: 'break-all' }}>{data.resetLink}</Text>

      <Alert variant='warning' style={{ marginTop: '20px' }}>
        <strong>Important:</strong> This link will expire in 1 hour. If you did not request this
        change, please ignore this email.
      </Alert>

      <CardFooter>
        <Text size='sm' style={{ marginBottom: '4px' }}>
          You&apos;re receiving this email because you have an account in {appName}. <br />
          If you are not sure why you&apos;re receiving this, please contact us.
        </Text>
      </CardFooter>
    </BaseTemplate>
  )
}

PasswordReset.TemplateProps = {
  ...sharedTemplateProps,
  data: {
    email: '{{.Data.Email}}',
    resetLink: '{{.Data.ResetLink}}'
  }
}

PasswordReset.PreviewProps = {
  ...sharedPreviewProps,
  data: {
    email: 'user@example.com',
    resetLink: 'https://example.com/reset-password?token=abc123'
  }
}

export default PasswordReset
