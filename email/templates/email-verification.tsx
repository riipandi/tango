import { Hr } from 'react-email'
import { Button, CardFooter, CardHeader, Text } from '../components'
import { sharedPreviewProps, sharedTemplateProps } from '../constants'
import { BaseTemplate } from '../layouts'

interface EmailVerificationData {
  userFullName: string
  verificationLink: string
}

interface EmailVerificationProps {
  logoURL: string
  appName: string
  data: EmailVerificationData
}

export const EmailVerification = ({ logoURL, appName, data }: EmailVerificationProps) => {
  return (
    <BaseTemplate logoURL={logoURL} appName={appName}>
      <CardHeader title='Email Verification' />
      <Hr style={{ marginTop: '16px' }} />
      <Text style={{ marginTop: '18px' }}>Hello {data.userFullName},</Text>

      <Text>Click the button below to verify your email address for {appName}.</Text>

      <Button href={data.verificationLink}>Verify Email</Button>

      <Text style={{ marginTop: '24px' }}>
        <strong>Important:</strong> This link will expire in 24 hours.
      </Text>

      <Text>If you did not create account, please ignore this email.</Text>

      <CardFooter>
        <Text size='sm' style={{ marginBottom: '4px' }}>
          You&apos;re receiving this email because you have an account in {appName}. <br />
          If you are not sure why you&apos;re receiving this, please contact us.
        </Text>
      </CardFooter>
    </BaseTemplate>
  )
}

EmailVerification.TemplateProps = {
  ...sharedTemplateProps,
  data: {
    userFullName: '{{.Data.UserFullName}}',
    verificationLink: '{{.Data.VerificationLink}}'
  }
}

EmailVerification.PreviewProps = {
  ...sharedPreviewProps,
  data: {
    userFullName: 'John Doe',
    verificationLink: 'https://localhost:3000/user/verify-email?code=abcdefg12345'
  }
}

export default EmailVerification
