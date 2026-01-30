import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../../utils/constants';
import UserSelect from '../../user-select';

const propTypes = {
  repoName: PropTypes.string.isRequired,
  toggle: PropTypes.func.isRequired,
  submit: PropTypes.func.isRequired,
};

class SysAdminRepoTransferDialog extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      selectedOption: null,
      errorMsg: [],
    };
  }

  handleSelectChange = (option) => {
    this.setState({selectedOption: option});
  };

  submit = () => {
    let user = this.state.selectedOption;
    this.props.submit(user);
  };

  render() {
    const repoName = this.props.repoName;
    const innerSpan = '<span class="op-target" title=' + repoName + '>' + repoName +'</span>';
    let msg = gettext('Transfer Library {library_name}');
    let message = msg.replace('{library_name}', innerSpan);
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title"><div dangerouslySetInnerHTML={{__html:message}} /></h5>
              <button type="button" className="close" onClick={this.props.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <UserSelect
            ref="userSelect"
            isMulti={false}
            className="reviewer-select"
            placeholder={gettext('Search users')}
            onSelectChange={this.handleSelectChange}
          />
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.submit}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

SysAdminRepoTransferDialog.propTypes = propTypes;

export default SysAdminRepoTransferDialog;
