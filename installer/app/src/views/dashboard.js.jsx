import Panel from './panel';
import InstallCert from './install-cert';

var InstallProgress = React.createClass({
	render: function () {
		return (
			<Panel>
				<InstallCert
					certURL={"data:application/x-x509-ca-cert;base64,"+ this.state.cert}
					dashboardURL={"https://dashboard."+ this.state.domain +"?token="+ encodeURIComponent(this.state.dashboardLoginToken)} />
			</Panel>
		);
	},

	getInitialState: function () {
		return this.__getState();
	},

	componentWillReceiveProps: function () {
		this.setState(this.__getState());
	},

	__getState: function () {
		return this.props.state;
	}
});
export default InstallProgress;
